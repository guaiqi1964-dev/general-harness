// GGUF 文件解析器（llama.cpp 模型格式，Go 版）。
// 只读元数据：magic + version + tensor_count + metadata KV 对。
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ggufUint8 = 0
	ggufInt8  = 1
	ggufUint16 = 2
	ggufInt16 = 3
	ggufUint32 = 4
	ggufInt32 = 5
	ggufFloat32 = 6
	ggufBool = 7
	ggufString = 8
	ggufArray = 9
	ggufUint64 = 10
	ggufInt64 = 11
	ggufFloat64 = 12
)

// GGUFReader 一个 GGUF 文件的元数据。
type GGUFReader struct {
	Path         string
	Version      uint32
	TensorCount  uint64
	Metadata     map[string]any
}

func openGGUF(path string) (*GGUFReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < 32 {
		return nil, fmt.Errorf("文件过小，不是合法 GGUF: %s", path)
	}
	magic := make([]byte, 4)
	if _, err := f.Read(magic); err != nil {
		return nil, err
	}
	if string(magic) != "GGUF" {
		return nil, fmt.Errorf("非法 GGUF 魔数: %q", magic)
	}
	reader := &GGUFReader{Path: path, Metadata: map[string]any{}}
	if err := binary.Read(f, binary.LittleEndian, &reader.Version); err != nil {
		return nil, err
	}
	if err := binary.Read(f, binary.LittleEndian, &reader.TensorCount); err != nil {
		return nil, err
	}
	var kvCount uint64
	if err := binary.Read(f, binary.LittleEndian, &kvCount); err != nil {
		return nil, err
	}
	if kvCount > 100000 {
		return nil, fmt.Errorf("KV 数量异常: %d", kvCount)
	}
	for i := uint64(0); i < kvCount; i++ {
		key, err := readGGUFString(f)
		if err != nil {
			return nil, err
		}
		val, err := readGGUFValue(f)
		if err != nil {
			return nil, err
		}
		reader.Metadata[key] = val
	}
	return reader, nil
}

func readGGUFString(f *os.File) (string, error) {
	var length uint64
	if err := binary.Read(f, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	if length > 1<<20 {
		return "", fmt.Errorf("字符串长度异常: %d", length)
	}
	buf := make([]byte, length)
	if _, err := f.Read(buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readGGUFValue(f *os.File) (any, error) {
	var vtype uint32
	if err := binary.Read(f, binary.LittleEndian, &vtype); err != nil {
		return nil, err
	}
	return readGGUFTyped(f, vtype)
}

func readGGUFTyped(f *os.File, vtype uint32) (any, error) {
	switch vtype {
	case ggufUint8:
		var v uint8
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufInt8:
		var v int8
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufUint16:
		var v uint16
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufInt16:
		var v int16
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufUint32:
		var v uint32
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufInt32:
		var v int32
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufFloat32:
		var v float32
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufBool:
		var v uint8
		err := binary.Read(f, binary.LittleEndian, &v)
		return v != 0, err
	case ggufString:
		return readGGUFString(f)
	case ggufArray:
		var elemType uint32
		if err := binary.Read(f, binary.LittleEndian, &elemType); err != nil {
			return nil, err
		}
		var count uint64
		if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
			return nil, err
		}
		if count > 10_000_000 {
			return nil, fmt.Errorf("数组长度异常: %d", count)
		}
		arr := make([]any, 0, count)
		for i := uint64(0); i < count; i++ {
			v, err := readGGUFTyped(f, elemType)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case ggufUint64:
		var v uint64
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufInt64:
		var v int64
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	case ggufFloat64:
		var v float64
		err := binary.Read(f, binary.LittleEndian, &v)
		return v, err
	default:
		return nil, fmt.Errorf("未知 GGUF value 类型: %d", vtype)
	}
}

// ---- 便捷访问 ----

func (r *GGUFReader) architecture() string {
	return toStr(r.Metadata["general.architecture"])
}

func (r *GGUFReader) name() string {
	if n := toStr(r.Metadata["general.name"]); n != "" {
		return n
	}
	return filepath.Base(r.Path)
}

func (r *GGUFReader) contextLength() int64 {
	for _, key := range []string{"llama.context_length", "qwen2.context_length",
		"mistral.context_length", "gpt2.context_length"} {
		if v, ok := r.Metadata[key]; ok {
			return toInt64(v)
		}
	}
	return 0
}

func (r *GGUFReader) vocabSize() int {
	if tokens, ok := r.Metadata["tokenizer.ggml.tokens"].([]any); ok {
		return len(tokens)
	}
	return 0
}

func (r *GGUFReader) toDict(fileSize int64) map[string]any {
	return map[string]any{
		"path":            r.Path,
		"name":            r.name(),
		"architecture":    r.architecture(),
		"version":         r.Version,
		"tensor_count":    r.TensorCount,
		"file_type":       toInt64(r.Metadata["general.file_type"]),
		"context_length":  r.contextLength(),
		"tokenizer_model": toStr(r.Metadata["tokenizer.ggml.model"]),
		"vocab_size":      r.vocabSize(),
		"file_size":       fileSize,
	}
}

// GGUFRegistry 扫描 models 目录。
type GGUFRegistry struct {
	dir    string
	Models map[string]*GGUFReader
}

func newGGUFRegistry(dir string) *GGUFRegistry {
	return &GGUFRegistry{dir: dir, Models: map[string]*GGUFReader{}}
}

func (r *GGUFRegistry) scan() {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gguf") {
			continue
		}
		path := filepath.Join(r.dir, e.Name())
		reader, err := openGGUF(path)
		if err != nil {
			continue // 跳过损坏文件
		}
		r.Models[reader.name()] = reader
	}
}

func (r *GGUFRegistry) list() []map[string]any {
	out := []map[string]any{}
	for _, reader := range r.Models {
		st, err := os.Stat(reader.Path)
		size := int64(0)
		if err == nil {
			size = st.Size()
		}
		out = append(out, reader.toDict(size))
	}
	return out
}

func (r *GGUFRegistry) get(name string) *GGUFReader {
	return r.Models[name]
}

func (r *GGUFRegistry) summary() []map[string]any {
	out := []map[string]any{}
	for _, reader := range r.Models {
		st, err := os.Stat(reader.Path)
		size := int64(0)
		if err == nil {
			size = st.Size()
		}
		out = append(out, map[string]any{
			"name":  reader.name(),
			"model": reader.name(),
			"size":  size,
			"details": map[string]any{
				"format": "gguf",
				"family": reader.architecture(),
			},
		})
	}
	return out
}
