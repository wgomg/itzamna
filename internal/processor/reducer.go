package processor

import (
	"cmp"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/utils"
)

func ReduceContent(content string, cfg *config.ReductionConfig) string {
	chunkSize := cfg.ChunkSize
	overlap := cfg.Overlap
	targetWordCount := cfg.TargetWords

	cleanedUpContent := utils.CleanUp(content)
	wordsArray := strings.Fields(cleanedUpContent)

	chunks := createChunks(wordsArray, chunkSize, overlap)

	chunks = tfScores(chunks)
	chunks = calculateGraphScores(chunks)
	chunks = calculateFinalScore(chunks, cfg.TfWeight, cfg.GraphWeight, cfg.PositionWeight)

	var selectedChunks []Chunk
	selectedChunks = make([]Chunk, 0)

	selectedChunks = append(selectedChunks, chunks[0])

	remainingChunks := make([]Chunk, len(chunks)-1)
	copy(remainingChunks, chunks[1:])
	slices.SortFunc(remainingChunks, cmpChunk)

	currentWordCount := chunks[0].WordCount

	for len(remainingChunks) > 0 && currentWordCount < targetWordCount {
		selected := remainingChunks[0]
		selectedChunks = append(selectedChunks, selected)
		currentWordCount += selected.WordCount

		remainingChunks = remainingChunks[1:]

		for i := range remainingChunks {
			similarity := jaccardSimilarity(selected.UniqueTokens, remainingChunks[i].UniqueTokens)

			if similarity > 0.15 {
				penalty := 1.0 - (similarity * 2.0)

				if penalty < 0.1 {
					penalty = 0.1
				}

				remainingChunks[i].FinalScore *= penalty
			}
		}

		slices.SortFunc(remainingChunks, cmpChunk)
	}

	slices.SortFunc(selectedChunks, func(a, b Chunk) int {
		return cmp.Compare(a.Id, b.Id)
	})

	reducedContent := selectedChunks[0].RawText

	for _, chunk := range selectedChunks[1:] {
		reducedContent += "\n"
		reducedContent += strings.Join(chunk.Words[overlap:], " ")
	}

	return reducedContent
}

func createChunks(words []string, chunkSize int, overlap int) []Chunk {
	stepSize := chunkSize - overlap
	totalChunks := (len(words) - overlap) / stepSize

	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)

	var chunks []Chunk

	for i := range totalChunks {
		startWordIndex := i * stepSize
		endWordIndex := startWordIndex + chunkSize

		chunkWords := words[startWordIndex:endWordIndex]

		var tokens []string
		for j := range len(chunkWords) {
			wordTokens := re.Split(chunkWords[j], -1)

			for l := range len(wordTokens) {
				if wordTokens[l] != "" {
					tokens = append(tokens, wordTokens[l])
				}
			}
		}

		tokenFrequencies := make(map[string]int)

		for k := range len(tokens) {
			tokenFrequencies[tokens[k]] = tokenFrequencies[tokens[k]] + 1
		}

		chunk := Chunk{
			Id:                 i,
			NormalizedPosition: normalize(float64(i), float64(totalChunks)),
			Words:              chunkWords,
			RawText:            strings.Join(chunkWords, " "),
			StartWordIndex:     startWordIndex,
			EndWordIndex:       endWordIndex,
			WordCount:          len(chunkWords),
			Tokens:             tokens,
			UniqueTokens:       slices.Collect(maps.Keys(tokenFrequencies)),
			TokenFrequencies:   tokenFrequencies,
		}

		chunks = append(chunks, chunk)
	}

	return chunks
}

func normalize(i float64, max float64) float64 {
	return i / max
}

func tfScores(chunks []Chunk) []Chunk {
	globalTokenFreq := make(map[string]int)
	for _, chunk := range chunks {
		for token, freq := range chunk.TokenFrequencies {
			globalTokenFreq[token] += freq
		}
	}

	var tfScores []float64
	for i := range chunks {
		chunk := &chunks[i]
		totalFreq := 0.0

		for token, localFreq := range chunk.TokenFrequencies {
			// using log1p to reduce dominance of very frequent terms
			totalFreq += math.Log1p(float64(globalTokenFreq[token])) * float64(localFreq)
		}

		chunk.TFScore = totalFreq / float64(len(chunk.Tokens))
		tfScores = append(tfScores, chunk.TFScore)
	}

	sumTF := 0.0
	for _, score := range tfScores {
		sumTF += score
	}

	for i := range chunks {
		chunk := &chunks[i]
		chunk.NormalizedTFScore = chunk.TFScore / sumTF
	}

	return chunks
}

func buildGraph(chunks []Chunk) Graph {
	chunksLength := len(chunks)
	graph := Graph{Nodes: make([]*Node, chunksLength), Adjacency: make([][]float64, chunksLength)}

	for i := range graph.Adjacency {
		graph.Adjacency[i] = make([]float64, chunksLength)
		graph.Nodes[i] = &Node{ID: i, Chunk: &chunks[i], Score: 0.0}

		// self-similarity
		graph.Adjacency[i][i] = 1.0
	}

	for i := range chunksLength {
		for j := i + 1; j < chunksLength; j++ {
			similarity := jaccardSimilarity(chunks[i].UniqueTokens, chunks[j].UniqueTokens)

			if similarity > 0 {
				graph.Adjacency[i][j] = similarity
				graph.Adjacency[j][i] = similarity
			}
		}
	}

	return graph
}

func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}

	set := make(map[string]bool)
	for _, v := range a {
		set[v] = true
	}

	intersection := 0
	for _, v := range b {
		if set[v] {
			intersection++
		}
	}

	// union = |A| + |B| - intersection
	union := len(a) + len(b) - intersection

	return float64(intersection) / float64(union)
}

func weightedPageRank(
	graph Graph,
	damping float64,
	maxIterations int,
	tolerance float64,
) []float64 {
	N := len(graph.Nodes)

	scores := make([]float64, N)
	for i := range scores {
		scores[i] = 1.0 / float64(N)
	}

	// precompute outgoing weigh sums for each node
	outgoingSums := make([]float64, N)
	for i := range N {
		sum := 0.0
		for j := range N {
			sum += graph.Adjacency[i][j]
		}
		outgoingSums[i] = sum
	}

	for range maxIterations {
		newScores := make([]float64, N)
		totalChange := 0.0

		randomComponent := (1.0 - damping) / float64(N)

		for i := range N {
			linkComponent := 0.0

			for j := range N {
				if i == j {
					continue
				}

				weight := graph.Adjacency[j][i]
				if weight > 0 && outgoingSums[j] > 0 {
					linkComponent += scores[j] * (weight / outgoingSums[j])
				}
			}

			newScores[i] = randomComponent + (damping * linkComponent)
			totalChange += math.Abs(newScores[i] - scores[i])
		}

		if totalChange < tolerance {
			break
		}

		scores = newScores
	}

	return scores
}

func calculateGraphScores(chunks []Chunk) []Chunk {
	graph := buildGraph(chunks)
	scores := weightedPageRank(graph, 0.85, 100, 0.0001)

	for i := range chunks {
		chunks[i].GraphScore = scores[i]
	}

	sumScores := 0.0
	for _, chunk := range chunks {
		sumScores += chunk.GraphScore
	}

	if sumScores > 0 {
		for i := range chunks {
			chunks[i].NormalizedGraphScore = chunks[i].GraphScore / sumScores
		}
	}

	return chunks
}

func calculateFinalScore(chunks []Chunk, tf_weight, graph_weight, position_weight float64) []Chunk {
	positionScores := make([]float64, len(chunks))
	for i := range chunks {
		pos := chunks[i].NormalizedPosition

		// position curve using cosine for smooth transitions
		// creates peaks at beginning (0.0) and center (0.5)
		score := math.Cos(pos * math.Pi * 2) // cosine wave with period 1.0
		score = math.Abs(score)              // make positive (0.0 to 1.0)
		score = 0.5 + (score * 0.5)          // scale to 0.5-1.0 range

		positionScores[i] = score
	}

	// normalize scores to sum 1.0
	sumPos := 0.0
	for _, score := range positionScores {
		sumPos += score
	}

	for i := range chunks {
		normalizedPosScore := 0.0
		if sumPos > 0 {
			normalizedPosScore = positionScores[i] / sumPos
		}

		chunks[i].FinalScore = (tf_weight * chunks[i].NormalizedTFScore) +
			(graph_weight * chunks[i].NormalizedGraphScore) +
			(position_weight * normalizedPosScore)
	}

	return chunks
}

func cmpChunk(a, b Chunk) int {
	return cmp.Compare(b.FinalScore, a.FinalScore)
}
