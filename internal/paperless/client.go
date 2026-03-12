package paperless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/utils"
	"github.com/wgomg/itzamna/internal/utils/httputils"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *utils.Logger
}

func NewClient(cfg *config.Config, logger *utils.Logger) (*Client, error) {
	if cfg.Paperless.URL == "" || cfg.Paperless.Token == "" {
		return nil, fmt.Errorf("PAPERLESS_URL and PAPERLESS_TOKEN are required")
	}

	return &Client{
		baseURL: cfg.Paperless.URL,
		token:   cfg.Paperless.Token,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.App.HttpTimeoutSeconds) * time.Second,
		},
		logger: logger,
	}, nil
}

func (c *Client) GetDocument(documentID int, reqID string) (*Document, error) {
	url := fmt.Sprintf("%s/api/documents/%d/", c.baseURL, documentID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeaders(req)

	c.logger.Debug(&reqID, "Fetching document %d from %s", documentID, c.baseURL)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch document: %w", err)
	}
	defer resp.Body.Close()

	_, err = httputils.LogResponseBody(resp, c.logger, reqID)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var document Document
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &document, nil
}

func (c *Client) GetDocumentsWithoutTags(reqID string) ([]Document, error) {
	url := fmt.Sprintf("%s/api/documents/?is_tagged=false", c.baseURL)
	var allUntaggedDocuments []Document

	c.logger.Debug(&reqID, "Fetching untagged documents from %s", url)
	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setAuthHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch untagged documents: %w", err)
		}
		defer resp.Body.Close()

		_, err = httputils.LogResponseBody(resp, c.logger, reqID)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, c.handleAPIError(resp)
		}

		var documentsResponse DocumentsResponse
		if err := json.NewDecoder(resp.Body).Decode(&documentsResponse); err != nil {
			return nil, err
		}

		allUntaggedDocuments = append(allUntaggedDocuments, documentsResponse.Results...)
		url = documentsResponse.Next
	}

	c.logger.Debug(&reqID, "Found %d untagged documents.", len(allUntaggedDocuments))

	return allUntaggedDocuments, nil
}

func (c *Client) GetDocumentTypes(reqID string) ([]DocumentType, error) {
	url := fmt.Sprintf("%s/api/document_types/", c.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeaders(req)

	c.logger.Debug(&reqID, "Fetching document types from %s", url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch document types: %w", err)
	}
	defer resp.Body.Close()

	_, err = httputils.LogResponseBody(resp, c.logger, reqID)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var dtResponse DocumentTypesResponse
	if err := json.NewDecoder(resp.Body).Decode(&dtResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Debug(&reqID, "Found %d document types.", dtResponse.Count)

	return dtResponse.Results, nil
}

func (c *Client) GetTag(tagName string, reqID string) (*Tag, error) {
	url := fmt.Sprintf("%s/api/tags/?name__iexact=%s", c.baseURL, tagName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeaders(req)

	c.logger.Debug(&reqID, "Fetching tag `%s` from %s", tagName, url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tag: %w", err)
	}
	defer resp.Body.Close()

	_, err = httputils.LogResponseBody(resp, c.logger, reqID)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var tagResponse TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(tagResponse.Results) == 0 {
		return nil, fmt.Errorf("tag `%s` not found", tagName)
	}

	return &tagResponse.Results[0], nil
}

func (c *Client) GetTags(reqID string) ([]Tag, error) {
	url := fmt.Sprintf("%s/api/tags/", c.baseURL)
	var allTags []Tag

	c.logger.Info(&reqID, "Fetching tags from %s", url)
	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setAuthHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tags: %w", err)
		}
		defer resp.Body.Close()

		_, err = httputils.LogResponseBody(resp, c.logger, reqID)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, c.handleAPIError(resp)
		}

		var tagsResponse TagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allTags = append(allTags, tagsResponse.Results...)
		url = tagsResponse.Next
	}

	c.logger.Info(&reqID, "Found %d tags.", len(allTags))

	return allTags, nil
}

func (c *Client) CreateTags(newTags []string, reqID string) (*CreateTagsResult, error) {
	url := fmt.Sprintf("%s/api/tags/", c.baseURL)

	result := &CreateTagsResult{
		CreatedTags: []Tag{},
		FailedTags:  []string{},
		Errors:      make(map[string]error),
	}

	for _, nt := range newTags {
		newTag := Tag{Name: nt, MatchingAlgorithm: 0, IsInboxTag: false}
		jsonNewTag, err := json.Marshal(newTag)
		if err != nil {
			result.FailedTags = append(result.FailedTags, nt)
			result.Errors[nt] = fmt.Errorf("failed to marshal new tag: %v", err)
			c.logger.Error(&reqID, "Failed to marshal tag '%s': %v", nt, err)
			continue
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonNewTag))
		if err != nil {
			result.FailedTags = append(result.FailedTags, nt)
			result.Errors[nt] = fmt.Errorf("failed to create request: %v", err)
			c.logger.Error(&reqID, "Failed to create request for tag '%s': %v", nt, err)
			continue
		}

		c.setAuthHeaders(req)
		req.Header.Set("Content-Type", "application/json")

		c.logger.Debug(&reqID, "Creating new paperless tag: %s", string(jsonNewTag))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			result.FailedTags = append(result.FailedTags, nt)
			result.Errors[nt] = fmt.Errorf("failed to create new tag: %v", err)
			c.logger.Error(&reqID, "Failed to create tag '%s': %v", nt, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusBadRequest {
				result.FailedTags = append(result.FailedTags, nt)
				result.Errors[nt] = fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
				c.logger.Error(&reqID, "Failed to create tag '%s': API error %d: %s",
					nt, resp.StatusCode, string(body))
				continue
			}

			c.logger.Debug(&reqID, "Tag `%s` already exists.", nt)

			tag, err := c.GetTag(nt, reqID)
			if err != nil {
				result.FailedTags = append(result.FailedTags, nt)
				result.Errors[nt] = fmt.Errorf("tag exists but failed to fetch: %v", err)
				c.logger.Error(&reqID, "Tag '%s' exists but failed to fetch: %v", nt, err)
				continue
			}

			result.CreatedTags = append(result.CreatedTags, *tag)
			c.logger.Debug(&reqID, "Using existing tag: %s (ID: %d)", tag.Name, tag.ID)
		} else {
			var createdTag Tag
			if err := json.NewDecoder(resp.Body).Decode(&createdTag); err != nil {
				result.FailedTags = append(result.FailedTags, nt)
				result.Errors[nt] = fmt.Errorf("failed to decode created tag response: %v", err)
				c.logger.Error(&reqID, "Failed to decode response for tag '%s': %v", nt, err)
				continue
			}
			c.logger.Debug(&reqID, "Successfully created tag: %s (ID: %d)",
				createdTag.Name, createdTag.ID)
			result.CreatedTags = append(result.CreatedTags, createdTag)
		}
	}

	if len(result.FailedTags) > 0 {
		c.logger.Debug(&reqID, "Tag creation completed with %d successes and %d failures: %v",
			len(result.CreatedTags), len(result.FailedTags), result.FailedTags)
	} else {
		c.logger.Info(&reqID, "All %d tags created successfully", len(result.CreatedTags))
	}

	return result, nil
}

func (c *Client) GetCorrespondents(name *string, reqID string) ([]Correspondent, error) {
	reqUrl := fmt.Sprintf("%s/api/correspondents/", c.baseURL)

	if name != nil {
		encName := url.QueryEscape(*name)
		reqUrl = fmt.Sprintf("%s?name__iexact=%s", reqUrl, encName)
	}

	var allCorrespondents []Correspondent

	c.logger.Debug(&reqID, "Fetching correspondents from %s", reqUrl)

	for reqUrl != "" {
		req, err := http.NewRequest("GET", reqUrl, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		c.setAuthHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch correspondents: %w", err)
		}
		defer resp.Body.Close()

		_, err = httputils.LogResponseBody(resp, c.logger, reqID)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, c.handleAPIError(resp)
		}

		var correspondentsResponse CorrespondentsResponse
		if err := json.NewDecoder(resp.Body).Decode(&correspondentsResponse); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allCorrespondents = append(allCorrespondents, correspondentsResponse.Results...)
		reqUrl = correspondentsResponse.Next
	}

	c.logger.Debug(&reqID, "Found %d correspondents.", len(allCorrespondents))

	return allCorrespondents, nil
}

func (c *Client) CreateCorrespondent(correspondent string, reqID string) (*Correspondent, error) {
	correspondents, err := c.GetCorrespondents(&correspondent, reqID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch existing correspondents: %w", err)
	}

	for _, corr := range correspondents {
		if corr.Name == correspondent {
			c.logger.Debug(&reqID, "Correspondent already exists: %s (ID: %d)", corr.Name, corr.ID)
			return &corr, nil
		}
	}

	url := fmt.Sprintf("%s/api/correspondents/", c.baseURL)

	newCorrespondent := Correspondent{Name: correspondent, MatchingAlgorithm: 0}
	jsonNewCorrespondent, err := json.Marshal(newCorrespondent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new correspondent: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonNewCorrespondent))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	c.logger.Debug(&reqID, "Creating new paperless correspondent: %s", string(jsonNewCorrespondent))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create new correspondent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create correspondent '%s': API error %d: %s",
			correspondent, resp.StatusCode, string(body))
	}

	var createdCorrespondent Correspondent
	if err := json.NewDecoder(resp.Body).Decode(&createdCorrespondent); err != nil {
		return nil, fmt.Errorf("failed to decode created correspondent response: %v", err)
	}
	c.logger.Debug(&reqID, "Successfully created correspondent: %s (ID: %d)",
		createdCorrespondent.Name, createdCorrespondent.ID)

	return &createdCorrespondent, nil
}

func (c *Client) UpdateDocument(documentID int, update *DocumentUpdate, reqID string) error {
	url := fmt.Sprintf("%s/api/documents/%d/", c.baseURL, documentID)

	body, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal update: %w", err)
	}

	maxRetries := 3
	var lastError error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		c.setAuthHeaders(req)
		req.Header.Set("Content-Type", "application/json")

		c.logger.Debug(
			&reqID,
			"Updating document ID=%d, Title=%s, DocumentType=%d, Correspondent=%d, Tags=%v - (attempt %d/%d)",
			documentID,
			update.Title,
			update.DocumentType,
			update.Correspondent,
			update.Tags,
			attempt,
			maxRetries,
		)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastError = fmt.Errorf("failed to update document (attempt: %d): %w", attempt, err)
			c.logger.Error(&reqID, "Update attempt %d failed: %v", attempt, err)

			if attempt < maxRetries {
				// exponential backoff: 2^attempt seconds
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				time.Sleep(backoff)
				continue
			}
			defer resp.Body.Close()
		}

		if resp.StatusCode == http.StatusOK {
			c.logger.Info(
				&reqID,
				"Document %d updated successfully (attempt %d)",
				documentID,
				attempt,
			)
			return nil
		}

		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusGatewayTimeout {
			lastError := c.handleAPIError(resp)

			c.logger.Error(&reqID, "Update attempt %d failed: %s",
				attempt, lastError.Error())

			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				time.Sleep(backoff)
				continue
			}
			return lastError
		}
	}

	return lastError
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", c.token))
}

func (c *Client) handleAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    http.StatusText(resp.StatusCode),
		Body:       string(body),
	}
}
