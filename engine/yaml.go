// 极简 YAML 子集解析器（零第三方依赖）。
// 支持本项目配置所需语法：注释、key: value、嵌套缩进 map、列表项、
// 字符串/数字/布尔值。不做 YAML 全标准支持。
package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type yamlValue = any // map[string]any | []any | string | float64 | bool | nil

func loadYAMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseYAML(string(data))
}

func parseYAML(src string) (map[string]any, error) {
	lines := strings.Split(src, "\n")
	root := map[string]any{}
	stack := []yamlValue{root}
	stackIndent := []int{-1}
	curKey := []string{""}

	for _, raw := range lines {
		if strings.Contains(raw, "\r") {
			raw = strings.ReplaceAll(raw, "\r", "")
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		// 弹出缩进大于当前的行
		for len(stackIndent) > 1 && indent <= stackIndent[len(stackIndent)-1] {
			stack = stack[:len(stack)-1]
			stackIndent = stackIndent[:len(stackIndent)-1]
			curKey = curKey[:len(curKey)-1]
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			// 列表项
			parent := stack[len(stack)-1]
			var list []any
			if l, ok := parent.([]any); ok {
				list = l
			} else if m, ok := parent.(map[string]any); ok {
				key := curKey[len(curKey)-1]
				list = []any{}
				m[key] = list
			} else {
				return nil, errors.New("YAML: 列表位置错误")
			}
			itemText := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if itemText == "" {
				item := map[string]any{}
				list = append(list, item)
				stack = append(stack, item)
				stackIndent = append(stackIndent, indent)
				curKey = append(curKey, "")
			} else if key, val, ok := splitKV(itemText); ok {
				item := map[string]any{key: parseScalar(val)}
				list = append(list, item)
				stack = append(stack, item)
				stackIndent = append(stackIndent, indent)
				curKey = append(curKey, key)
			} else {
				list = append(list, parseScalar(itemText))
			}
			// 写回列表
			parent = stack[len(stack)-2]
			if m, ok := parent.(map[string]any); ok {
				m[curKey[len(curKey)-1]] = list
			}
			continue
		}
		key, val, ok := splitKV(trimmed)
		if !ok {
			return nil, errors.New("YAML: 无法解析行: " + trimmed)
		}
		parent := stack[len(stack)-1]
		m, ok := parent.(map[string]any)
		if !ok {
			return nil, errors.New("YAML: 键值对位于列表内: " + trimmed)
		}
		if strings.TrimSpace(val) == "" {
			child := map[string]any{}
			m[key] = child
			stack = append(stack, child)
			stackIndent = append(stackIndent, indent)
			curKey = append(curKey, key)
		} else {
			m[key] = parseScalar(strings.TrimSpace(val))
			curKey[len(curKey)-1] = key
		}
	}
	return root, nil
}

func splitKV(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(line[idx+1:]), true
}

func parseScalar(s string) any {
	if s == "" {
		return ""
	}
	if s[0] == '"' && len(s) > 1 && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	if s[0] == '\'' && len(s) > 1 && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	if s == "true" || s == "True" {
		return true
	}
	if s == "false" || s == "False" {
		return false
	}
	if s == "null" || s == "~" {
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if strings.Contains(s, ".") {
			return f
		}
		return f
	}
	if s[0] == '[' || s[0] == '{' {
		return s
	}
	return s
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
	}
	return def
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}
