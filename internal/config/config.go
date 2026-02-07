package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Environment string

const (
	Development Environment = "development"
	Production  Environment = "production"
)

type AppConfig struct {
	Env                Environment
	LogLevel           string
	ServerPort         string
	RawBodyLog         bool
	HttpTimeoutSeconds int
}

type PaperlessConfig struct {
	URL   string
	Token string
}

type LlmConfig struct {
	URL              string
	Token            string
	Model            string
	Temperature      float64
	MaxTokens        int
	FrequencyPenalty float64
	PresencePenalty  float64
}

type PythonConfig struct {
	ConfigDir              string
	ProcessStartupDelay    int
	ProcessShutdownTimeout int
	ProcessKillTimeout     int
}

type SemanticConfig struct {
	TopN          int
	MinSimilarity float64
	TimeoutMs     int
	Model         string
	TagsThreshold int
	WorkerCount   int
	Python        PythonConfig
}

type ReductionConfig struct {
	ThresholdTokens    int
	ChunkSize          int
	Overlap            int
	TargetWords        int
	TfWeight           float64
	GraphWeight        float64
	PositionWeight     float64
	DiversityThreshold float64
	MinPenalty         float64
}

type Config struct {
	App       AppConfig
	Paperless PaperlessConfig
	Llm       LlmConfig
	Semantic  SemanticConfig
	Reduction ReductionConfig
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	appEnv := getEnv("APP_ENV", "development")
	env := parseEnvironment(appEnv)

	logLevel := getLogLevel(env)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	defaultPythonDir := filepath.Join(homeDir, ".config", "itzamna")

	defaultWorkerCount := calculateDefaultWorkerCount()

	return &Config{
		App: AppConfig{
			Env:                env,
			LogLevel:           logLevel,
			ServerPort:         getEnv("APP_SERVER_PORT", "8080"),
			RawBodyLog:         getEnvBool("APP_RAW_BODY_LOG", false),
			HttpTimeoutSeconds: getEnvInt("APP_HTTP_TIMEOUT_SECONDS", 30),
		},
		Paperless: PaperlessConfig{
			URL:   getEnv("PAPERLESS_URL", ""),
			Token: getEnv("PAPERLESS_TOKEN", ""),
		},
		Llm: LlmConfig{URL: getEnv("LLM_URL", ""), Token: getEnv("LLM_TOKEN", ""),
			Model:            getEnv("LLM_MODEL", ""),
			Temperature:      getEnvFloat("LLM_TEMPERATURE", 0.6),
			MaxTokens:        getEnvInt("LLM_MAX_TOKENS", 2000),
			FrequencyPenalty: getEnvFloat("LLM_FREQUENCY_PENALTY", 0.0),
			PresencePenalty:  getEnvFloat("LLM_PRESENCE_PENALTY", 0.0)},
		Semantic: SemanticConfig{
			TopN:          getEnvInt("SEMANTIC_TOP_N", 15),
			MinSimilarity: getEnvFloat("SEMANTIC_MIN_SIMILARITY", 0.2),
			TimeoutMs:     getEnvInt("SEMANTIC_TIMEOUT_MS", 10000),
			Model:         getEnv("SEMANTIC_MODEL_NAME", "all-MiniLM-L6-v2"),
			TagsThreshold: getEnvInt("SEMANTIC_TAGS_THRESHOLD", 15),
			WorkerCount:   getEnvInt("SEMANTIC_WORKER_COUNT", defaultWorkerCount),
			Python: PythonConfig{
				ConfigDir:              getEnv("SEMANTIC_PYTHON_CONFIG_DIR", defaultPythonDir),
				ProcessStartupDelay:    getEnvInt("SEMANTIC_PYTHON_PROCESS_STARTUP_DELAY", 2),
				ProcessShutdownTimeout: getEnvInt("SEMANTIC_PYTHON_PROCESS_SHUTDOWN_TIMEOUT", 5),
				ProcessKillTimeout:     getEnvInt("SEMANTIC_PYTHON_PROCESS_KILL_TIMEOUT", 2),
			},
		},
		Reduction: ReductionConfig{
			ThresholdTokens:    getEnvInt("REDUCTION_THRESHOLD_TOKENS", 2000),
			ChunkSize:          getEnvInt("REDUCTION_CHUNK_SIZE", 150),
			Overlap:            getEnvInt("REDUCTION_OVERLAP", 15),
			TargetWords:        getEnvInt("REDUCTION_TARGET_WORDS", 1150),
			TfWeight:           getEnvFloat("REDUCTION_TF_WEIGHT", 0.4),
			GraphWeight:        getEnvFloat("REDUCTION_GRAPH_WEIGHT", 0.4),
			PositionWeight:     getEnvFloat("REDUCTION_POSITION_WEIGHT", 0.2),
			DiversityThreshold: getEnvFloat("REDUCTION_DIVERSITY_THRESHOLD", 0.15),
			MinPenalty:         getEnvFloat("REDUCTION_MIN_PENALTY", 0.1),
		},
	}, nil
}

func (c *Config) Validate() error {
	if c.Paperless.URL == "" || c.Paperless.Token == "" {
		return fmt.Errorf("PAPERLESS_URL and PAPERLESS_TOKEN are required")
	}
	if c.Llm.URL == "" || c.Llm.Token == "" {
		return fmt.Errorf("LLM_URL and LLM_TOKEN are required")
	}
	return nil
}

func parseEnvironment(envStr string) Environment {
	env := Environment(strings.ToLower(envStr))

	switch env {
	case Development, Production:
		return env
	default:
		return Development
	}
}

func calculateDefaultWorkerCount() int {
	cpuCores := runtime.NumCPU()

	// estimate model memory usage (conservative estimate for default model)
	// all-MiniLM-L6-v2: ~90MB, multilingual models: 120-420MB
	// using 200MB as conservative estimate
	modelMemoryMB := 200

	var availableMemoryMB int64 = 4096 // default to 4GB

	// trying to read system memory
	if memInfo, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(memInfo), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						availableMemoryMB = kb / 1024
						break
					}
				}
			}
		}
	}

	workersByCPU := min(cpuCores, 6)

	// calculate based on memory (leave 2GB for system and Go process)
	systemReservedMB := 2048
	usableMemoryMB := int(availableMemoryMB) - systemReservedMB
	if usableMemoryMB < 0 {
		usableMemoryMB = 2048
	}

	workersByMemory := max(min(usableMemoryMB/modelMemoryMB, 6), 1)
	workerCount := min(max(min(workersByMemory, workersByCPU), 1), 6)

	return workerCount
}

func getLogLevel(env Environment) string {
	if env == Production {
		return getEnv("APP_LOG_LEVEL", "info")
	}

	return getEnv("APP_LOG_LEVEL", "debug")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value == "true" {
		return true
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultValue
}
