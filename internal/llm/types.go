package llm

import "fmt"

type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages         []ChatMessage   `json:"messages"`
	Model            string          `json:"model"`
	Thinking         *ThinkingConfig `json:"thinking,omitempty"`
	FrequencyPenalty float64         `json:"frequency_penalty"`
	MaxTokens        int             `json:"max_tokens"`
	PresencePenalty  float64         `json:"presence_penalty"`
	ResponseFormat   ResponseFormat  `json:"response_format"`
	Stop             interface{}     `json:"stop"`
	Stream           bool            `json:"stream"`
	StreamOptions    interface{}     `json:"stream_options"`
	Temperature      float64         `json:"temperature"`
	TopP             float64         `json:"top_p"`
	Tools            interface{}     `json:"tools"`
	ToolChoice       string          `json:"tool_choice"`
	Logprobs         bool            `json:"logprobs"`
	TopLogprobs      interface{}     `json:"top_logprobs"`
}

type ThinkingConfig struct {
	Type string `json:"type"`
}

type ResponseFormat struct {
	Type string `json:"type"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Object  string   `json:"object"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	FinishReason string  `json:"finish_reason"`
	Index        int     `json:"index"`
	Message      Message `json:"message"`
}

type Message struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type Usage struct {
	CompletionTokens int `json:"completion_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type AnalysisResult struct {
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Tags     []string `json:"tags"`
	Author   string   `json:"author"`
	Language string   `json:"language"`
}
