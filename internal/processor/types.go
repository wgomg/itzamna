package processor

type Chunk struct {
	Id                   int
	NormalizedPosition   float64
	RawText              string
	Words                []string
	StartWordIndex       int
	EndWordIndex         int
	WordCount            int
	Tokens               []string
	UniqueTokens         []string
	TokenFrequencies     map[string]int
	TFScore              float64
	NormalizedTFScore    float64
	GraphScore           float64
	NormalizedGraphScore float64
	FinalScore           float64
}

type Node struct {
	ID    int
	Chunk *Chunk
	Score float64
}

type Graph struct {
	Nodes     []*Node
	Adjacency [][]float64
}
