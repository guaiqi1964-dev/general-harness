// Ollama 兼容层单元测试。
package main

import "testing"

// TestStreamChatOllamaFinalDone 云端流式也必须以 done:true 收尾，
// 否则 Ollama 客户端会一直等待结束标记。
func TestStreamChatOllamaFinalDone(t *testing.T) {
	p := &Provider{Name: "fake", BaseURL: "http://x", Keys: []APIKey{{Name: "default", Key: "sk"}}}
	p.Models = []string{"fake-model"}
	p.StreamHandler = func(p *Provider, model string, messages []map[string]any,
		keySelector string, temperature *float64, maxTokens *int, topP *float64,
		streamOptions map[string]any, onChunk func(map[string]any) error) error {
		if err := onChunk(map[string]any{"id": "s1", "content": "你"}); err != nil {
			return err
		}
		return onChunk(map[string]any{"id": "s1", "content": "好"})
	}
	cloud := &CloudRegistry{Providers: map[string]*Provider{"fake": p}}
	o := newOllamaAdapter(cloud, newGGUFRegistry(t.TempDir()), nil)

	var blocks []map[string]any
	err := o.streamChatOllama("fake/fake-model",
		[]map[string]any{{"role": "user", "content": "hi"}}, nil, "",
		func(b map[string]any) error { blocks = append(blocks, b); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) < 3 {
		t.Fatalf("blocks = %d, want >= 3 (2 content + 1 done)", len(blocks))
	}
	last := blocks[len(blocks)-1]
	if done, _ := last["done"].(bool); !done {
		t.Errorf("last block done = %v, want true; blocks=%v", last["done"], blocks)
	}
	if msg, ok := blocks[0]["message"].(map[string]any); !ok || msg["content"] != "你" {
		t.Errorf("first block message = %v", blocks[0]["message"])
	}
}

// TestStreamChatOllamaLocalDone 本地 GGUF 流式同样以 done:true 收尾。
func TestStreamChatOllamaLocalDone(t *testing.T) {
	dir := t.TempDir()
	gguf := newGGUFRegistry(dir)
	// 注入一个伪 GGUFReader，使 isLocal 命中本地分支。
	gguf.Models["demo"] = &GGUFReader{Path: dir, Metadata: map[string]any{}}
	o := newOllamaAdapter(&CloudRegistry{Providers: map[string]*Provider{}}, gguf, nil)

	var blocks []map[string]any
	err := o.streamChatOllama("demo", nil, nil, "",
		func(b map[string]any) error { blocks = append(blocks, b); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) < 2 {
		t.Fatalf("blocks = %d, want >= 2", len(blocks))
	}
	if done, _ := blocks[len(blocks)-1]["done"].(bool); !done {
		t.Errorf("last block done = %v, want true", blocks[len(blocks)-1]["done"])
	}
}
