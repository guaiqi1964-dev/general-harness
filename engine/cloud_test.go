// 云端 HTTP 客户端测试。
package main

import (
	"io"
	"strings"
	"testing"
)

func TestChunkedReader(t *testing.T) {
	data := "5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
	cr := newChunkedReader(strings.NewReader(data))
	out, err := io.ReadAll(cr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello world" {
		t.Errorf("got %q, want %q", out, "hello world")
	}
}

func TestChunkedReaderMulti(t *testing.T) {
	data := "1\r\na\r\n1\r\nb\r\n1\r\nc\r\n0\r\n\r\n"
	cr := newChunkedReader(strings.NewReader(data))
	out, err := io.ReadAll(cr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "abc" {
		t.Errorf("got %q, want %q", out, "abc")
	}
}
