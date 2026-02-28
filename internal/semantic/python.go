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
	RequestID string
	Text      string
	NewTags   []string
	Result    chan<- TaskResult
}

type TaskResult struct {
	Tags []string
	Err  error
}

type PythonMatcher struct {
	logger    *utils.Logger
	script    string
	venv      string
	cfg       *config.SemanticConfig
	process   *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	scanner   *bufio.Scanner
	mu        sync.Mutex
	taskQueue chan Task
	closed    bool
}

type PythonRequest struct {
	Text    string   `json:"text"`
	NewTags []string `json:"new_tags"`
}

type PythonResponse struct {
	SuggestedTags []string            `json:"suggested_tags"`
	Error         *string             `json:"error"`
	DebugInfo     PythonResponseDebug `json:"debug_info"`
}

type PythonResponseDebug struct {
	ProcessingTimeMS    int `json:"processing_time_ms"`
	TotalTagsConsidered int `json:"total_tags_considered"`
	TagsAboveThreshold  int `json:"tags_above_threshold"`
}

func NewPythonMatcher(logger *utils.Logger, cfg *config.SemanticConfig) *PythonMatcher {
	pythonDir := filepath.Join(cfg.Python.ConfigDir, "python")
	script := filepath.Join(pythonDir, "semantic_matcher.py")
	venv := filepath.Join(cfg.Python.ConfigDir, "venv")

	return &PythonMatcher{
		logger:    logger,
		script:    script,
		venv:      venv,
		cfg:       cfg,
		taskQueue: make(chan Task, 100),
	}
}

func (p *PythonMatcher) Initialize() error {
	p.logger.Info(nil, "Initializing Python semantic matcher")

	if err := p.setupEnvironment(); err != nil {
		return fmt.Errorf("failed to setup environment: %w", err)
	}

	if err := p.startProcess(); err != nil {
		return fmt.Errorf("failed to start python process: %w", err)
	}

	go p.handleRequests()

	p.logger.Info(nil, "Python semantic matcher initialized successfully")
	return nil
}

func (p *PythonMatcher) GetTagSuggestions(
	text string,
	newTags []string,
	reqID string,
) ([]string, error) {
	if newTags == nil {
		newTags = []string{}
	}

	result := make(chan TaskResult, 1)
	task := Task{
		Text:      text,
		NewTags:   newTags,
		RequestID: reqID,
		Result:    result,
	}

	p.taskQueue <- task
	res := <-result
	return res.Tags, res.Err
}

func (p *PythonMatcher) startProcess() error {
	python := filepath.Join(p.venv, "bin", "python")

	cmd := exec.Command(python, p.script)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("start process: %w", err)
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
		return fmt.Errorf("marshal config: %w", err)
	}

	configJSON = append(configJSON, '\n')
	if _, err := stdin.Write(configJSON); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("send config: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to read READY message")
	}

	var readyMsg struct {
		Status       string `json:"status"`
		EmbeddingDim int    `json:"embedding_dim"`
	}
	if err := json.Unmarshal([]byte(scanner.Text()), &readyMsg); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to parse ready message: %w", err)
	}

	if readyMsg.Status != "ready" {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("unexpected startup status: %s", readyMsg.Status)
	}

	p.process = cmd
	p.stdin = stdin
	p.stdout = stdout
	p.scanner = scanner

	p.logger.Info(nil, "Python matcher ready (embedding_dim=%d)", readyMsg.EmbeddingDim)

	return nil
}

func (p *PythonMatcher) handleRequests() {
	for task := range p.taskQueue {
		p.processTask(task)
	}
}

func (p *PythonMatcher) processTask(task Task) {
	if p.closed {
		task.Result <- TaskResult{Err: fmt.Errorf("python matcher closed")}
	}

	req := PythonRequest{
		Text:    task.Text,
		NewTags: task.NewTags,
	}

	if req.NewTags == nil {
		req.NewTags = []string{}
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		task.Result <- TaskResult{Err: fmt.Errorf("marshal request: %w", err)}
		return
	}

	reqJSON = append(reqJSON, '\n')
	if _, err := p.stdin.Write(reqJSON); err != nil {
		task.Result <- TaskResult{Err: fmt.Errorf("write request: %w", err)}
		return
	}

	if p.scanner.Scan() {
		var resp PythonResponse
		if err := json.Unmarshal([]byte(p.scanner.Text()), &resp); err != nil {
			task.Result <- TaskResult{Err: fmt.Errorf("parse response: %w", err)}
			return
		}
		if resp.Error != nil && *resp.Error != "" {
			task.Result <- TaskResult{Err: fmt.Errorf("python error: %s", *resp.Error)}
			return
		}

		p.logger.Info(
			&task.RequestID,
			"Semantic matcher stats: process_ms=%d, total_tags=%d, tags_above_threshold=%d",
			resp.DebugInfo.ProcessingTimeMS,
			resp.DebugInfo.TotalTagsConsidered,
			resp.DebugInfo.TagsAboveThreshold,
		)
		task.Result <- TaskResult{Tags: resp.SuggestedTags}
		return
	}

	if err := p.scanner.Err(); err != nil {
		task.Result <- TaskResult{Err: fmt.Errorf("read stdout: %w", err)}
		return
	}

	task.Result <- TaskResult{Err: fmt.Errorf("stdout closed")}
}

func (p *PythonMatcher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	close(p.taskQueue)

	if p.stdin != nil {
		p.stdin.Close()
	}
	if p.process != nil {
		p.process.Process.Kill()
	}
	if p.stdout != nil {
		p.stdout.Close()
	}
}

func (p *PythonMatcher) setupEnvironment() error {
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

func (p *PythonMatcher) checkPython() error {
	cmd := exec.Command("python3", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("python3 not found: %w", err)
	}

	p.logger.Debug(nil, "Python3 found")
	return nil
}

func (p *PythonMatcher) createVenv() error {
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

func (p *PythonMatcher) installRequirements() error {
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

func (p *PythonMatcher) extractScriptIfNeeded() error {
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

func (p *PythonMatcher) HealthCheck() error {
	testTags := []string{"test", "document", "invoice"}

	result := make(chan TaskResult, 1)
	task := Task{
		Text:    "test document for health check",
		NewTags: testTags,
		Result:  result,
	}

	p.taskQueue <- task

	res := <-result
	if res.Err != nil {
		return fmt.Errorf("health check: worker error: %w", res.Err)
	}
	return nil
}
