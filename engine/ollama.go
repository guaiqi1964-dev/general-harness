// Ollama 协议兼容层（Go 版）：把 Ollama 风格请求映射到统一引擎。
package main

import (
	"encoding/json"
	"strings"
)

// OllamaAdapter 把统一引擎能力封装为 Ollama 协议。
type OllamaAdapter struct {
	Cloud   *CloudRegistry
	GGUF    *GGUFRegistry
	Aliases map[string]string
}

func newOllamaAdapter(cloud *CloudRegistry, gguf *GGUFRegistry, aliases map[string]string) *OllamaAdapter {
	return &OllamaAdapter{Cloud: cloud, GGUF: gguf, Aliases: aliases}
}

func (o *OllamaAdapter) isLocal(model string) bool {
	return o.GGUF.get(model) != nil
}

func (o *OllamaAdapter) listModels() map[string]any {
	models := []map[string]any{}
	models = append(models, o.GGUF.summary()...)
	for _, item := range o.Cloud.modelList() {
		mid := toStr(item["id"])
		models = append(models, map[string]any{
			"name":    mid,
			"model":   mid,
			"size":    int64(0),
			"details": map[string]any{"format": "openai", "family": toStr(item["owned_by"])},
		})
	}
	return map[string]any{"models": models}
}

// chatOllama 非流式 /api/chat，返回 Ollama 格式响应。
func (o *OllamaAdapter) chatOllama(model string, messages []map[string]any,
	options map[string]any, keySelector string) (map[string]any, error) {
	if o.isLocal(model) {
		reply := localGGUFReply(o.GGUF.get(model))
		return map[string]any{
			"message": map[string]any{"role": "assistant", "content": reply},
			"done":    true,
		}, nil
	}
	provider, actual, err := o.Cloud.resolve(model, o.Aliases)
	if err != nil {
		return nil, err
	}
	resp, err := provider.chatCompletion(actual, messages, keySelector,
		optFloat(options, "temperature"), optInt(options, "num_predict"))
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"message": map[string]any{"role": "assistant", "content": toStr(resp["content"])},
		"done":    true,
		"model":   orDefault(toStr(resp["model"]), model),
	}
	if usage, ok := resp["usage"].(map[string]any); ok {
		out["prompt_eval_count"] = usage["prompt_tokens"]
		out["eval_count"] = usage["completion_tokens"]
	}
	return out, nil
}

// streamChatOllama 流式 /api/chat，逐块回调 Ollama 格式块。
func (o *OllamaAdapter) streamChatOllama(model string, messages []map[string]any,
	options map[string]any, keySelector string, emit func(map[string]any) error) error {
	if o.isLocal(model) {
		reply := localGGUFReply(o.GGUF.get(model))
		if err := emit(map[string]any{"message": map[string]any{"role": "assistant", "content": reply}, "done": false}); err != nil {
			return err
		}
		return emit(map[string]any{"done": true})
	}
	provider, actual, err := o.Cloud.resolve(model, o.Aliases)
	if err != nil {
		return err
	}
	err = provider.streamChatCompletion(actual, messages, keySelector,
		optFloat(options, "temperature"), optInt(options, "num_predict"),
		func(chunk map[string]any) error {
			if c, ok := chunk["content"].(string); ok && c != "" {
				return emit(map[string]any{"message": map[string]any{"role": "assistant", "content": c}, "done": false})
			}
			return nil
		})
	// 云端流式也必须以 done:true 收尾，否则 Ollama 客户端会一直等待结束标记。
	if err != nil {
		if pe, ok := err.(*PluginError); ok {
			_ = emit(map[string]any{"error": pe.Message, "done": true})
		} else {
			_ = emit(map[string]any{"error": "内部错误", "done": true})
		}
		return err
	}
	return emit(map[string]any{"done": true})
}

func localGGUFReply(reader *GGUFReader) string {
	if reader == nil {
		return "[本地 GGUF] 未知模型"
	}
	var b strings.Builder
	b.WriteString("[本地 GGUF 模型] " + reader.name() + "\n")
	b.WriteString("架构: " + reader.architecture() + " · 上下文: " +
		itoa64(reader.contextLength()) + " · 词表: " + itoa(reader.vocabSize()) + "\n")
	b.WriteString("本引擎仅提供 GGUF 解析能力，不含本地推理。")
	return b.String()
}

func optFloat(options map[string]any, key string) *float64 {
	if options == nil {
		return nil
	}
	if v, ok := options[key]; ok {
		if f, ok := toFloat64(v); ok {
			return &f
		}
	}
	return nil
}

func optInt(options map[string]any, key string) *int {
	if options == nil {
		return nil
	}
	if v, ok := options[key]; ok {
		if f, ok := toFloat64(v); ok {
			n := int(f)
			return &n
		}
	}
	return nil
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}
