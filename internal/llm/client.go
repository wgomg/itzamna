package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/paperless"
	"github.com/wgomg/itzamna/internal/utils"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *utils.Logger
	cfg        *config.LlmConfig
}

func NewClient(cfg *config.Config, logger *utils.Logger) (*Client, error) {
	if cfg.Llm.URL == "" || cfg.Llm.Token == "" {
		return nil, fmt.Errorf("LLM_URL and LLM_TOKEN are required")
	}

	return &Client{
		baseURL: cfg.Llm.URL,
		token:   cfg.Llm.Token,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.App.HttpTimeoutSeconds) * time.Second,
		},
		logger: logger,
		cfg:    &cfg.Llm,
	}, nil
}

func (c *Client) AnalyzeContent(
	content string,
	pages int,
	documentTypes []paperless.DocumentType,
	tags []string,
) (*AnalysisResult, error) {
	typesString := ""
	if len(documentTypes) > 0 {
		types := make([]string, len(documentTypes))
		for i, dt := range documentTypes {
			types[i] = dt.Name
		}
		typesString = fmt.Sprintf(
			"- Document type: choose one of '%s', also take document's page count into account for deciding document type.",
			strings.Join(types, ","),
		)
	}

	tagsString := fmt.Sprintf(
		" Prefer tags from the following list if thematically related to document excerpts: '%s'",
		strings.Join(tags, ","),
	)

	if len(documentTypes) > 0 {
		tagsString += fmt.Sprintf(
			" DO NOT use words from the following list as tags: '%s'",
			typesString,
		)
	}

	prompt := fmt.Sprintf(
		"Analyze the excerpts of a document provided below and extract the following data: \n- Document title: In excerpts language, truncate to 127 characters if longer\n%s\n- Tags: At most five thematic tags. English only, lowercase, prefer single word, if two or more words join them with hyphens.%s\n- Author: full name of author, correspondent or incumbent dependening of document type, if more than one name then in a comma separated list.\n- Language: 3 letters code, set as 'und' if unable to identify.\nReturn ONLY a json string without any explanations, numbers, additional text, text formatting or text/code blocks, with keys: title, type, tags, author, language.\n\nDocument's page count: %d\n\nDocument Excerpts: %s",
		typesString,
		tagsString,
		pages,
		content,
	)

	reqBody := ChatRequest{
		Messages: []ChatMessage{
			{
				Role:    "system",
				Content: "You are a helpful assistant specialized in document analysis and metadata extraction",
			}, {
				Role: "user", Content: prompt,
			},
		},
		Model:            c.cfg.Model,
		Thinking:         &ThinkingConfig{Type: "disabled"},
		FrequencyPenalty: c.cfg.FrequencyPenalty,
		MaxTokens:        c.cfg.MaxTokens,
		PresencePenalty:  c.cfg.PresencePenalty,
		ResponseFormat:   ResponseFormat{Type: "text"},
		Stop:             nil,
		Stream:           false,
		StreamOptions:    nil,
		Temperature:      c.cfg.Temperature,
		TopP:             1,
		Tools:            nil,
		ToolChoice:       "none",
		Logprobs:         false,
		TopLogprobs:      nil,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	c.logger.Debug("Sending LLM request: %s", string(jsonBody))

	req, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Debug("LLM usage - prompt_tokens: %d, completion_tokens: %d, total_tokens: %d",
		chatResp.Usage.PromptTokens,
		chatResp.Usage.CompletionTokens,
		chatResp.Usage.TotalTokens)

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("empty response from LLM")
	}

	responseContent := chatResp.Choices[0].Message.Content
	responseContent = strings.TrimSpace(responseContent)

	c.logger.Debug("LLM raw response: %s", responseContent)

	var analysisResult AnalysisResult
	if err := json.Unmarshal([]byte(responseContent), &analysisResult); err != nil {
		return nil, fmt.Errorf("LLM returned invalid JSON: %w", err)
	}

	return &analysisResult, nil
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
}

func (c *Client) handleAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    http.StatusText(resp.StatusCode),
		Body:       string(body),
	}
}
