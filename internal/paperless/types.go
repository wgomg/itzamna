package paperless

import "fmt"

type Document struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	DocumentType  int    `json:"document_type"`
	Tags          []int  `json:"tags"`
	Correspondent int    `json:"correspondent"`
	PageCount     int    `json:"page_count"`
}

type DocumentsResponse struct {
	Count   int        `json:"count"`
	Results []Document `json:"results"`
	Next    string     `json:"next"`
}

type DocumentType struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type DocumentTypesResponse struct {
	Count   int            `json:"count"`
	Results []DocumentType `json:"results"`
}

type Tag struct {
	ID                int    `json:"id,omitempty"`
	Name              string `json:"name"`
	MatchingAlgorithm int    `json:"matching_algorithm"`
	IsInboxTag        bool   `json:"is_inbox_tag"`
}

type TagsResponse struct {
	Count   int    `json:"count"`
	Results []Tag  `json:"results"`
	Next    string `json:"next"`
}

type Correspondent struct {
	ID                int    `json:"id,omitempty"`
	Name              string `json:"name"`
	MatchingAlgorithm int    `json:"matching_algorithm"`
}

type CorrespondentsResponse struct {
	Count   int             `json:"count"`
	Results []Correspondent `json:"results"`
	Next    string          `json:"next"`
}

type DocumentUpdate struct {
	Title         string `json:"title"`
	DocumentType  int    `json:"document_type"`
	Tags          []int  `json:"tags"`
	Correspondent int    `json:"correspondent"`
}

type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}
