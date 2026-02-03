package api

import "net/http"

func RegisterRoutes(mux *http.ServeMux, handler *Handler) {
	mux.HandleFunc("POST /webhook", handler.HandleWebhook)
	mux.HandleFunc("POST /process/untagged", handler.HandleProcessUntagged)
}
