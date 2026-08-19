// Agent 命令执行器：在安全约束下执行宿主系统命令，供模型通过 HTTP API 调用。
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AgentConfig Agent 命令执行配置。
type AgentConfig struct {
	Enabled        bool
	AllowCommands  []string
	TimeoutSeconds int
	MaxOutputBytes int
	WorkDir        string
}

// AgentResult 命令执行结果（统一 JSON 响应）。
type AgentResult struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	ExitCode  int      `json:"exit_code"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	TimedOut  bool     `json:"timed_out"`
	Truncated bool     `json:"truncated"`
}

// AgentExecutor 命令执行器。
type AgentExecutor struct {
	cfg *AgentConfig
}

func newAgentExecutor(cfg *AgentConfig) *AgentExecutor {
	return &AgentExecutor{cfg: cfg}
}

func (a *AgentExecutor) enabled() bool {
	return a.cfg != nil && a.cfg.Enabled
}

func (a *AgentExecutor) timeout() int {
	if a.cfg != nil && a.cfg.TimeoutSeconds > 0 {
		return a.cfg.TimeoutSeconds
	}
	return 30
}

func (a *AgentExecutor) maxOutput() int {
	if a.cfg != nil && a.cfg.MaxOutputBytes > 0 {
		return a.cfg.MaxOutputBytes
	}
	return 64 * 1024
}

func (a *AgentExecutor) workDir() string {
	if a.cfg != nil && a.cfg.WorkDir != "" {
		return a.cfg.WorkDir
	}
	if ROOT != "" {
		return ROOT
	}
	return "."
}

// allowed 校验可执行文件名是否在白名单中（忽略大小写、路径与扩展名）。
func (a *AgentExecutor) allowed(command string) bool {
	if a.cfg == nil || len(a.cfg.AllowCommands) == 0 {
		return false
	}
	base := command
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	for _, allowed := range a.cfg.AllowCommands {
		if strings.EqualFold(base, allowed) {
			return true
		}
	}
	return false
}

// Run 执行命令。返回 error 表示被拒绝（未启用 / 不在白名单 / 执行失败）。
func (a *AgentExecutor) Run(command string, args []string, timeoutSec int) (*AgentResult, error) {
	if !a.enabled() {
		return nil, newPluginError("Agent 命令执行未启用（config.yaml 中 agent.enabled=false）", 403, "permission_error")
	}
	if command == "" {
		return nil, newPluginError("command 不能为空", 400, "invalid_request_error")
	}
	if !a.allowed(command) {
		return nil, newPluginError("命令不在白名单中: "+command, 403, "permission_error")
	}
	timeout := a.timeout()
	if timeoutSec > 0 {
		timeout = timeoutSec
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = a.workDir()
	cmd.Env = sanitizedEnv()
	var stdout, stderr limitedBuffer
	stdout.limit = a.maxOutput()
	stderr.limit = a.maxOutput()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	res := &AgentResult{Command: command, Args: args}
	err := cmd.Run()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	res.Truncated = stdout.truncated || stderr.truncated
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		return res, nil
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			return nil, newPluginError("命令执行失败: "+err.Error(), 500, "internal_error")
		}
	}
	return res, nil
}

// limitedBuffer 限制输出大小的缓冲。
type limitedBuffer struct {
	buf       []byte
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.buf) >= b.limit {
		b.truncated = true
		return len(p), nil
	}
	room := b.limit - len(b.buf)
	n := len(p)
	if n > room {
		n = room
		b.truncated = true
	}
	b.buf = append(b.buf, p[:n]...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

// sanitizedEnv 返回去除敏感变量后的环境（隔离 API Key / Token 等）。
func sanitizedEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, e := range env {
		key := e
		if idx := strings.Index(e, "="); idx >= 0 {
			key = e[:idx]
		}
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "CREDENTIAL") {
			continue
		}
		out = append(out, e)
	}
	return out
}
