package semantic

type Embedding []float64

type Model interface {
	EmbedText(text string) (Embedding, error)
	EmbeddingDimension() int
	Close() error
}

type TagSimilarity struct {
	Tag   string
	Score float64
}

type Matcher interface {
	GetTagSuggestions(text string, existingTags []string) ([]string, error)
	HealthCheck() error
	Close() error
}

type WorkerPool interface {
	Matcher
}
