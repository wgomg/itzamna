package api

type WebhookPayload struct {
	DocumentURL string `json:"document_url"`
}

type WebhookResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}
