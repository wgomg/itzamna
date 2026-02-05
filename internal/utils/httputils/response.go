package httputils

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/wgomg/itzamna/internal/utils"
)

func JSONResponse(w http.ResponseWriter, status int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

func JSONError(w http.ResponseWriter, status int, message string) error {
	return JSONResponse(w, status, map[string]string{
		"error": message,
	})
}

func SuccessResponse(w http.ResponseWriter, message string, data any) error {
	response := map[string]any{
		"status":  "success",
		"message": message,
	}
	if data != nil {
		response["data"] = data
	}
	return JSONResponse(w, http.StatusOK, response)
}

func LogResponseBody(resp *http.Response, logger *utils.Logger, reqID string) ([]byte, error) {
	if !logger.RawBodyLog {
		return nil, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	logger.Debug(&reqID, "Raw response body: %s", string(bodyBytes))

	return bodyBytes, nil
}
