package httputils

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/wgomg/itzamna/internal/utils"
)

func DecodeJSON(r *http.Request, v any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return &HTTPError{
			Code:    http.StatusUnsupportedMediaType,
			Message: "Content-Type must be application/json",
		}
	}

	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return &HTTPError{
			Code:    http.StatusBadRequest,
			Message: "Invalid JSON payload: " + err.Error(),
		}
	}
	return nil
}

func ValidateMethod(r *http.Request, allowedMethod string) error {
	if r.Method != allowedMethod {
		return &HTTPError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		}
	}
	return nil
}

func LogRequestBody(r *http.Request, logger *utils.Logger, reqID string) ([]byte, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	logger.Debug(&reqID, "Raw request body: %s", string(bodyBytes))

	return bodyBytes, nil
}
