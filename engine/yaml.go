// YAML 配置解析（手写子集解析器，零第三方依赖）。
//
// 覆盖本项目配置所需的 YAML 形态：嵌套 map、标量列表、列表嵌套 map
// （如 api_keys 的 name/key 结构）、注释、单/双引号字符串，以及整数、
// 浮点、布尔、空值标量。解析结果统一为 map[string]any / []any。
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func loadYAMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseYAMLString(string(data))
}

func parseYAMLString(src string) (map[string]any, error) {
	src = strings.TrimPrefix(src, "\ufeff")
	p := &yamlParser{lines: tokenizeYAML(src)}
	v, err := p.parseNode(0)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

// ---- 词法：按行切分，剥离注释与空行，记录缩进 ----

type yline struct {
	indent int    // 行首空格/制表符数量
	text   string // 去掉缩进与注释后的内容
}

func tokenizeYAML(src string) []yline {
	var lines []yline
	for _, raw := range strings.Split(src, "\n") {
		raw = strings.TrimRight(raw, "\r")
		text := stripYAMLComment(raw)
		indent := 0
		for indent < len(text) && (text[indent] == ' ' || text[indent] == '\t') {
			indent++
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || trimmed == "---" || trimmed == "..." {
			continue
		}
		lines = append(lines, yline{indent: indent, text: trimmed})
	}
	return lines
}

// stripYAMLComment 去掉行尾注释（引号内的 # 视为字面量，不剥离）。
func stripYAMLComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '#' && !inS && !inD:
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i]
			}
		}
	}
	return s
}

// ---- 递归下降解析 ----

type yamlParser struct {
	lines []yline
	pos   int
}

func (p *yamlParser) parseNode(indent int) (any, error) {
	if p.pos >= len(p.lines) {
		return nil, nil
	}
	line := p.lines[p.pos]
	if line.indent < indent {
		return nil, nil // 缩进回退，交还上层
	}
	if line.indent > indent {
		return nil, fmt.Errorf("YAML 缩进错误（第 %d 行）", p.pos+1)
	}
	if isSeqItem(line.text) {
		return p.parseSequence(indent)
	}
	return p.parseMapping(indent)
}

func isSeqItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func (p *yamlParser) parseMapping(indent int) (map[string]any, error) {
	m := map[string]any{}
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, fmt.Errorf("YAML 缩进错误（第 %d 行）", p.pos+1)
		}
		if isSeqItem(line.text) {
			break
		}
		key, rest, err := splitKeyValue(line.text)
		if err != nil {
			return nil, err
		}
		rest = strings.TrimSpace(rest)
		if rest != "" {
			m[key] = parseScalar(rest)
			p.pos++
			continue
		}
		// 无内联值：子块（更深的缩进）。
		p.pos++
		if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
			v, err := p.parseNode(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			m[key] = v
		} else {
			m[key] = nil
		}
	}
	return m, nil
}

func (p *yamlParser) parseSequence(indent int) ([]any, error) {
	seq := []any{}
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, fmt.Errorf("YAML 缩进错误（第 %d 行）", p.pos+1)
		}
		if !isSeqItem(line.text) {
			break
		}
		item := strings.TrimSpace(line.text[1:]) // 去掉开头的 '-'
		if item == "" {
			p.pos++
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				v, err := p.parseNode(p.lines[p.pos].indent)
				if err != nil {
					return nil, err
				}
				seq = append(seq, v)
			} else {
				seq = append(seq, nil)
			}
			continue
		}
		if k, rest, ok := trySplitKeyValue(item); ok {
			// "- key: value"：列表项是一个 map。
			m, err := p.parseSeqMapItem(indent, k, rest)
			if err != nil {
				return nil, err
			}
			seq = append(seq, m)
			continue
		}
		// 标量列表项。
		seq = append(seq, parseScalar(item))
		p.pos++
	}
	return seq, nil
}

// parseSeqMapItem 解析 "- key: value"，并把后续缩进更深的键并入同一 map
// （对应 api_keys 中每个条目下的 name/key 等多字段）。
func (p *yamlParser) parseSeqMapItem(indent int, firstKey, firstRest string) (map[string]any, error) {
	m := map[string]any{}
	rest := strings.TrimSpace(firstRest)
	if rest != "" {
		m[firstKey] = parseScalar(rest)
		p.pos++
	} else {
		p.pos++
		if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
			v, err := p.parseNode(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			m[firstKey] = v
		} else {
			m[firstKey] = nil
		}
	}
	for p.pos < len(p.lines) {
		l2 := p.lines[p.pos]
		if l2.indent <= indent {
			break
		}
		if isSeqItem(l2.text) {
			break
		}
		k2, rest2, err := splitKeyValue(l2.text)
		if err != nil {
			return nil, err
		}
		rest2 = strings.TrimSpace(rest2)
		if rest2 != "" {
			m[k2] = parseScalar(rest2)
			p.pos++
			continue
		}
		p.pos++
		if p.pos < len(p.lines) && p.lines[p.pos].indent > l2.indent {
			v, err := p.parseNode(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			m[k2] = v
		} else {
			m[k2] = nil
		}
	}
	return m, nil
}

// splitKeyValue 在第一个顶层 ':' 处拆分为 key / rest（引号内的 ':' 忽略）。
func splitKeyValue(text string) (string, string, error) {
	k, rest, ok := trySplitKeyValue(text)
	if !ok {
		return "", "", fmt.Errorf("无法解析为键值对: %s", text)
	}
	return k, rest, nil
}

func trySplitKeyValue(text string) (string, string, bool) {
	inS, inD := false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == ':' && !inS && !inD:
			return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+1:]), true
		}
	}
	return "", "", false
}

// parseScalar 把标量文本转换为对应 Go 类型。
func parseScalar(s string) any {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	switch strings.ToLower(s) {
	case "", "~", "null":
		return nil
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// ---- 取值辅助 ----

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
		if f, ok := v.(int64); ok {
			return int(f)
		}
	}
	return def
}

func yamlBool(m map[string]any, key string, def bool) bool {
	if v, ok := m[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
		if s, ok := v.(string); ok {
			return s == "true" || s == "yes" || s == "1"
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
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}
