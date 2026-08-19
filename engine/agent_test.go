// Agent 命令执行器单元测试。
package main

import (
	"strings"
	"testing"
	"time"
)

func TestAgentAllowed(t *testing.T) {
	a := newAgentExecutor(&AgentConfig{Enabled: true, AllowCommands: []string{"cmd", "powershell", "ping"}})
	if !a.allowed("cmd") {
		t.Error("cmd should be allowed")
	}
	if !a.allowed("CMD.EXE") {
		t.Error("CMD.EXE should be allowed (case-insensitive)")
	}
	if !a.allowed("C:/Windows/System32/ping.exe") {
		t.Error("full path ping.exe should be allowed")
	}
	if a.allowed("notallowed") {
		t.Error("notallowed should be denied")
	}
}

func TestAgentDisabled(t *testing.T) {
	a := newAgentExecutor(&AgentConfig{Enabled: false})
	if _, err := a.Run("cmd", nil, 0); err == nil {
		t.Error("disabled agent should error")
	}
}

func TestAgentNotAllowed(t *testing.T) {
	a := newAgentExecutor(&AgentConfig{Enabled: true, AllowCommands: []string{"ping"}})
	if _, err := a.Run("del", nil, 0); err == nil {
		t.Error("del should be denied")
	}
}

func TestAgentRunEcho(t *testing.T) {
	a := newAgentExecutor(&AgentConfig{Enabled: true, AllowCommands: []string{"cmd"}, TimeoutSeconds: 30, MaxOutputBytes: 65536})
	res, err := a.Run("cmd", []string{"/c", "echo hello"}, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Errorf("stdout = %q, want contains hello", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
}

func TestAgentTimeout(t *testing.T) {
	a := newAgentExecutor(&AgentConfig{Enabled: true, AllowCommands: []string{"powershell"}, TimeoutSeconds: 1})
	start := time.Now()
	res, err := a.Run("powershell", []string{"-Command", "Start-Sleep 5"}, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.TimedOut {
		t.Error("should time out")
	}
	if time.Since(start) > 8*time.Second {
		t.Error("timeout took too long")
	}
}

func TestAgentOutputTruncation(t *testing.T) {
	a := newAgentExecutor(&AgentConfig{Enabled: true, AllowCommands: []string{"cmd"}, MaxOutputBytes: 32})
	long := strings.Repeat("a", 100)
	res, err := a.Run("cmd", []string{"/c", "echo " + long}, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Truncated {
		t.Error("should be truncated")
	}
	if len(res.Stdout) > 32 {
		t.Errorf("stdout len = %d, want <= 32", len(res.Stdout))
	}
}
