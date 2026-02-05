package semantic

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/utils"
)

func NewMatcher(logger *utils.Logger, cfg *config.SemanticConfig) (Matcher, error) {
	pythonDir := filepath.Join(cfg.Python.ConfigDir, "python")
	scriptPath := filepath.Join(pythonDir, "semantic_matcher.py")

	if _, err := os.Stat(scriptPath); err != nil {
		devScriptPath := filepath.Join("scripts", "semantic_matcher.py")
		if _, err := os.Stat(devScriptPath); err == nil {
			scriptPath = devScriptPath
			logger.Info(nil, "Using development Python script at %s", scriptPath)
		} else {
			logger.Info(nil, "Python script will be extracted from embedded resources")
		}
	} else {
		logger.Info(nil, "Using existing Python script at %s", scriptPath)
	}

	matcher := NewPythonMatcher(logger, cfg)

	if err := matcher.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize python matcher: %w", err)
	}

	return matcher, nil
}
