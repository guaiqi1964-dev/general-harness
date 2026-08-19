package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStreamingUsageRecording(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"fake/fake-model","messages":[{"role":"user","content":"hi"}],"stream":true,"session_id":"test-sess"}`
	fmt.Fprintf(conn, "POST /v1/chat/completions HTTP/1.1\nHost: %s\nContent-Type: application/json\nContent-Length: %d\nConnection: close\n\n%s", addr, len(body), body)
	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	for {
		hl, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.TrimSpace(hl) == "" {
			break
		}
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	conn.Close()
	if !strings.Contains(sb.String(), "[DONE]") {
		t.Fatalf("stream body = %q", sb.String())
	}
	status, stats := httpGet(t, addr, "/v1/usage/stats?limit=10")
	if status != 200 {
		t.Fatalf("stats = %d", status)
	}
	data, ok := stats["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("stats data = %v", stats["data"])
	}
	conv, _ := data[0].(map[string]any)
	if conv["session_id"] != "test-sess" {
		t.Errorf("session_id = %v, want test-sess", conv["session_id"])
	}
	if conv["tokens"] != float64(3) {
		t.Errorf("tokens = %v, want 3", conv["tokens"])
	}
}

func TestParseUsageDeepSeekFormat(t *testing.T) {
	// DeepSeek usage 块：choices 为空数组，usage 为对象
	usageChunk := map[string]any{
		"id":      "s1",
		"choices": []any{},
		"usage":   map[string]any{"prompt_tokens": float64(1), "completion_tokens": float64(2), "total_tokens": float64(3)},
		"model":   "deepseek-chat",
	}
	u := chunkToUnifiedResponse(usageChunk)
	usage, ok := u["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage 缺失: %v", u["usage"])
	}
	if toInt64(usage["total_tokens"]) != 3 {
		t.Errorf("total_tokens = %v, want 3", usage["total_tokens"])
	}
	// 普通流式块 usage 为 null → parseUsage 应返回 nil
	normalChunk := map[string]any{
		"id":    "s1",
		"choices": []any{map[string]any{"index": float64(0), "delta": map[string]any{"content": "你"}, "finish_reason": nil}},
		"usage": nil,
		"model": "deepseek-chat",
	}
	if pu := parseUsage(normalChunk); pu != nil {
		t.Errorf("parseUsage(usage=null) = %v, want nil", pu)
	}
}