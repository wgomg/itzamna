package main

import (
	"net/http"

	"github.com/wgomg/itzamna/internal/api"
	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/llm"
	"github.com/wgomg/itzamna/internal/paperless"
	"github.com/wgomg/itzamna/internal/semantic"
	"github.com/wgomg/itzamna/internal/utils"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log := utils.NewLogger("error", cfg.App.RawBodyLog)
		log.Fatal("Failed to load configuration:", err)
	}
	if err := cfg.Validate(); err != nil {
		log := utils.NewLogger("error", cfg.App.RawBodyLog)
		log.Fatal("Invalid configuration:", err)
	}

	logger := utils.NewLogger(cfg.App.LogLevel, cfg.App.RawBodyLog)
	logger.Info(nil, "Starting Document Processing Service")
	logger.Info(nil, "Environment: %s", cfg.App.Env)
	logger.Info(nil, "Log level: %s", cfg.App.LogLevel)
	logger.Info(nil, "Python config directory: %s", cfg.Semantic.Python.ConfigDir)

	paperlessClient, err := paperless.NewClient(cfg, logger)
	if err != nil {
		logger.Error(nil, "Failed to create Paperless client: %v", err)
		logger.Fatal(nil, "Missing required configuration")
	}
	llmClient, err := llm.NewClient(cfg, logger)
	if err != nil {
		logger.Error(nil, "Failed to create LLM client: %v", err)
		logger.Fatal("Missing required configuration")
	}
	semanticMatcher, err := semantic.NewMatcher(logger, &cfg.Semantic)
	if err != nil {
		logger.Error(nil, "Failed to create semantic matcher: %v", err)
		logger.Fatal("Failed to initialize semantic matcher")
	}
	defer semanticMatcher.Close()

	webhookhandler := api.NewHandler(logger, paperlessClient, llmClient, semanticMatcher, cfg)

	mux := api.RegisterRoutes(webhookhandler)

	logger.Info(nil, "Starting server on port %s", cfg.App.ServerPort)
	logger.Info(nil, "Endpoints:")
	logger.Info(nil, "  GET  /health")
	logger.Info(nil, "  POST /webhook")
	logger.Info(nil, "  POST /process/untagged")
	logger.Fatal(http.ListenAndServe("0.0.0.0:"+cfg.App.ServerPort, mux))
}
