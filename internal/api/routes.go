package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

func RegisterRoutes(handler *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Document Processing Service is running\n")
	})

	mux.HandleFunc("POST /webhook", handler.HandleWebhook)
	mux.HandleFunc("POST /process/untagged", handler.HandleProcessUntagged)

	return requestMiddleware(handler, mux)
}

func requestMiddleware(handler *Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.New().String()

		ctx := context.WithValue(r.Context(), "reqid", reqID)

		handler.logger.Info(nil, "%s %s REQID=%s", r.Method, r.URL.Path, reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
