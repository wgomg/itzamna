package semantic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/utils"
)

type Task struct {
	RequestID    string
	Text         string
	ExistingTags []string
	Result       chan<- TaskResult
}

type TaskResult struct {
	Tags []string
	Err  error
}

type PythonWorkerPool struct {
	logger    *utils.Logger
	script    string
	venv      string
	cfg       *config.SemanticConfig
	taskQueue chan Task
	wg        sync.WaitGroup
}

type PythonWorker struct {
	id      int
	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	mu      sync.Mutex
	pool    *PythonWorkerPool
}

type PythonRequest struct {
	Text         string   `json:"text"`
	ExistingTags []string `json:"existing_tags"`
}

type PythonResponse struct {
	SuggestedTags []string             `json:"suggested_tags"`
	Error         string               `json:"error,omitempty"`
	DebugInfo     *PythonResponseDebug `json:"debug_info"`
}

type PythonResponseDebug struct {
	ProcessingTimeMS    int                       `json:"processing_time_ms"`
	TotalTagsConsidered int                       `json:"total_tags_considered"`
	TagsAboveThreshold  int                       `json:"tags_above_threshold"`
	CacheStats          *PythonResponseCacheStats `json:"cache_stats"`
	NewlyCachedTags     int                       `json:"newly_cached_tags"`
}

type PythonResponseCacheStats struct {
	CacheSize      int     `json:"cache_size"`
	TotalHits      int     `json:"total_hits"`
	TotalMisses    int     `json:"total_misses"`
	TotalHitRate   float64 `json:"total_hit_rate"`
	RequestHits    int     `json:"request_hits"`
	RequestMisses  int     `json:"request_misses"`
	RequestHitRate float64 `json:"request_hit_rate"`
}

func NewPythonMatcher(logger *utils.Logger, cfg *config.SemanticConfig) *PythonWorkerPool {
	pythonDir := filepath.Join(cfg.Python.ConfigDir, "python")
	script := filepath.Join(pythonDir, "semantic_matcher.py")
	venv := filepath.Join(cfg.Python.ConfigDir, "venv")

	return &PythonWorkerPool{
		logger:    logger,
		script:    script,
		venv:      venv,
		cfg:       cfg,
		taskQueue: make(chan Task, 100),
	}
}

func (p *PythonWorkerPool) Initialize() error {
	p.logger.Info(nil, "Initializing Python semantic matcher with %d workers", p.cfg.WorkerCount)

	if err := p.setupEnvironment(); err != nil {
		return fmt.Errorf("failed to setup environment: %w", err)
	}

	for i := 0; i < p.cfg.WorkerCount; i++ {
		p.wg.Add(1)
		go p.runWorker(i)
	}

	p.logger.Info(nil, "Python semantic matcher initialized successfully")
	return nil
}

func (p *PythonWorkerPool) GetTagSuggestions(
	text string,
	existingTags []string,
	reqID *string,
) ([]string, error) {
	result := make(chan TaskResult, 1)
	task := Task{
		Text:         text,
		ExistingTags: existingTags,
		RequestID:    *reqID,
		Result:       result,
	}

	p.taskQueue <- task

	res := <-result
	return res.Tags, res.Err
}

func (p *PythonWorkerPool) runWorker(id int) {
	defer p.wg.Done()

	worker, err := p.startWorker(id)
	if err != nil {
		p.logger.Error(nil, "Failed to start worker %d: %v", id, err)
		return
	}
	defer worker.close()

	for task := range p.taskQueue {
		if err := worker.processTask(task); err != nil {
			task.Result <- TaskResult{Err: err}
			return
		}
	}
}

func (p *PythonWorkerPool) startWorker(id int) (*PythonWorker, error) {
	python := filepath.Join(p.venv, "bin", "python")

	cmd := exec.Command(python, p.script)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}

	config := map[string]interface{}{
		"model_name":           p.cfg.Model,
		"top_n":                p.cfg.TopN,
		"min_similarity":       p.cfg.MinSimilarity,
		"normalize_embeddings": true,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	configJSON = append(configJSON, '\n')
	if _, err := stdin.Write(configJSON); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("send config: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to read READY message")
	}

	var readyMsg struct {
		Status       string `json:"status"`
		EmbeddingDim int    `json:"embedding_dim"`
	}
	if err := json.Unmarshal([]byte(scanner.Text()), &readyMsg); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to parse ready message: %w", err)
	}

	if readyMsg.Status != "ready" {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("unexpected startup status: %s", readyMsg.Status)
	}

	p.logger.Debug(nil, "Python worker %d ready (embedding_dim=%d)", id, readyMsg.EmbeddingDim)

	return &PythonWorker{
		id:      id,
		process: cmd,
		stdin:   stdin,
		stdout:  stdout,
		pool:    p,
	}, nil
}

func (w *PythonWorker) processTask(task Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	scanner := bufio.NewScanner(w.stdout)

	req := PythonRequest{
		Text:         task.Text,
		ExistingTags: task.ExistingTags,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	reqJSON = append(reqJSON, '\n')
	if _, err := w.stdin.Write(reqJSON); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	if scanner.Scan() {
		var resp PythonResponse
		if err := json.Unmarshal([]byte(scanner.Text()), &resp); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
		if resp.Error != "" {
			return fmt.Errorf("python error: %s", resp.Error)
		}

		w.pool.logger.Info(
			&task.RequestID,
			"Semantic matcher stats: process_ms=%d, new_tags=%d, cache_size=%d, total_cache_hit_rate=%f",
			resp.DebugInfo.ProcessingTimeMS,
			resp.DebugInfo.NewlyCachedTags,
			resp.DebugInfo.CacheStats.CacheSize,
			resp.DebugInfo.CacheStats.TotalHitRate,
		)

		w.pool.logger.Debug(
			&task.RequestID,
			"Semantic matcher stats: process_ms=%d, new_tags=%d, cache_size=%d, total_cache_hits=%d, total_cache_misses=%d, total_cache_hit_rate=%f, request_cache_hits=%d request_cache_hit_misses=%d, request_cache_hit_rate=%f",
			resp.DebugInfo.ProcessingTimeMS,
			resp.DebugInfo.NewlyCachedTags,
			resp.DebugInfo.CacheStats.CacheSize,
			resp.DebugInfo.CacheStats.TotalHits,
			resp.DebugInfo.CacheStats.TotalMisses,
			resp.DebugInfo.CacheStats.TotalHitRate,
			resp.DebugInfo.CacheStats.RequestHits,
			resp.DebugInfo.CacheStats.RequestMisses,
			resp.DebugInfo.CacheStats.RequestHitRate,
		)
		task.Result <- TaskResult{Tags: resp.SuggestedTags}
		return nil
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdout: %w", err)
	}
	return fmt.Errorf("stdout closed")
}

func (w *PythonWorker) close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stdin != nil {
		w.stdin.Close()
	}
	if w.process != nil {
		w.process.Process.Kill()
	}
	if w.stdout != nil {
		w.stdout.Close()
	}
}

func (p *PythonWorkerPool) Close() error {
	close(p.taskQueue)
	p.wg.Wait()
	return nil
}

func (p *PythonWorkerPool) setupEnvironment() error {
	if err := os.MkdirAll(p.cfg.Python.ConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := p.extractScriptIfNeeded(); err != nil {
		return fmt.Errorf("failed to extract script: %w", err)
	}

	if err := p.checkPython(); err != nil {
		return fmt.Errorf("python check failed: %w", err)
	}

	if err := p.createVenv(); err != nil {
		return fmt.Errorf("failed to create venv: %w", err)
	}

	if err := p.installRequirements(); err != nil {
		return fmt.Errorf("failed to install requirements: %w", err)
	}

	return nil
}

func (p *PythonWorkerPool) checkPython() error {
	cmd := exec.Command("python3", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("python3 not found: %w", err)
	}

	p.logger.Debug(nil, "Python3 found")
	return nil
}

func (p *PythonWorkerPool) createVenv() error {
	venvPython := filepath.Join(p.venv, "bin", "python")

	if _, err := os.Stat(venvPython); err == nil {
		p.logger.Debug(nil, "Virtual environment already exists at %s", p.venv)
		return nil
	}

	p.logger.Info(nil, "Creating virtual environment at %s", p.venv)

	cmd := exec.Command("python3", "-m", "venv", p.venv)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create venv: %s: %w", output, err)
	}

	p.logger.Info(nil, "Virtual environment created successfully")
	return nil
}

func (p *PythonWorkerPool) installRequirements() error {
	venvPip := filepath.Join(p.venv, "bin", "pip")

	p.logger.Info(nil, "Installing Python requirements")

	requirements := []string{
		"sentence-transformers>=2.2.2",
		"torch>=2.0.0",
		"numpy>=1.21.0",
	}

	for _, req := range requirements {
		p.logger.Debug(nil, "Installing: %s", req)

		cmd := exec.Command(venvPip, "install", req)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to install %s: %s: %w", req, output, err)
		}
	}

	p.logger.Info(nil, "Python requirements installed successfully")
	return nil
}

func (p *PythonWorkerPool) extractScriptIfNeeded() error {
	pythonDir := filepath.Join(p.cfg.Python.ConfigDir, "python")

	if err := os.MkdirAll(pythonDir, 0755); err != nil {
		return fmt.Errorf("failed to create python directory: %w", err)
	}

	if _, err := os.Stat(p.script); err == nil {
		p.logger.Debug(nil, "Python script already exists at %s", p.script)
		return nil
	}

	p.logger.Info(nil, "Extracting embedded Python script to %s", p.script)

	if err := os.WriteFile(p.script, []byte(embeddedPythonScript), 0755); err != nil {
		return fmt.Errorf("failed to write python script: %w", err)
	}

	requirementsPath := filepath.Join(pythonDir, "requirements.txt")
	requirementsContent := embeddedRequirements
	if requirementsContent == "" {
		requirementsContent = defaultRequirements
	}

	if err := os.WriteFile(requirementsPath, []byte(requirementsContent), 0644); err != nil {
		return fmt.Errorf("failed to write requirements file: %w", err)
	}

	p.logger.Info(nil, "Python script extracted successfully")
	return nil
}

func (p *PythonWorkerPool) HealthCheck() error {
	testTags := []string{"test", "document", "invoice"}

	result := make(chan TaskResult, 1)
	task := Task{
		Text:         "test document for health check",
		ExistingTags: testTags,
		Result:       result,
	}

	p.taskQueue <- task

	res := <-result
	if res.Err != nil {
		return fmt.Errorf("health check: worker error: %w", res.Err)
	}
	return nil
}
