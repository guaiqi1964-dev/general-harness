// YAML 配置解析（gopkg.in/yaml.v3 标准实现）。
// 早期版本使用手写 YAML 子集解析器，但嵌套 map 列表（如 api_keys、
// models）的归属逻辑存在缺陷，会导致列表丢项与结构错乱。
// 为保证配置解析的正确性，改用经过验证的标准库。
package main

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type yamlValue = any

func loadYAMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseYAMLString(string(data))
}

func parseYAMLString(src string) (map[string]any, error) {
	var raw any
	if err := yaml.Unmarshal([]byte(src), &raw); err != nil {
		return nil, err
	}
	m, ok := normalize(raw).(map[string]any)
	if !ok || m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

// normalize 把 yaml.v3 的 map[string]interface{}/[]interface{} 结构统一为
// map[string]any / []any（去除类型差异），保证上层类型断言稳定。
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[toStr(k)] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	default:
		return v
	}
}

func yamlStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return toStr(v)
	}
	return ""
}

func yamlInt(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok && v != nil {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		if f, ok := v.(int); ok {
			return f
		}
	}
	return def
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	default:
		return strings.TrimSpace(strings.ReplaceAll(sprintfYAML(v), "\n", " "))
	}
}

func sprintfYAML(v any) string {
	data, _ := yaml.Marshal(v)
	return string(data)
}
