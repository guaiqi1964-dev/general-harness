package main

import (
	_ "embed"
	"strconv"
)

// uiHTML 是内置浏览器界面（GET /ui）。
//
// 存在理由：安装包不捆绑 Python，而 gui/gui.py + pywebview 需要 Python 3.10+。
// 在没有 Python（或没有 pywebview）的机器上，/ui 让用户直接用浏览器获得
// 与桌面 GUI 等价的对话体验——模型选择、流式输出、思考折叠、联网开关与来源卡片、用量查询。
//
// 用 go:embed 编进二进制，因此不引入任何外部文件依赖，也不改变「单二进制」哲学。
//
//go:embed ui.html
var uiHTML string

// handleUI 返回内置页面。静态资源不做鉴权：页面本身不含敏感数据，
// 其中的 API 调用（/v1/*、/api/*）仍受 gateway_api_key 保护，用户可在页面右上角填入 Key。
func (e *Engine) handleUI(req *httpRequest, w *responseWriter) {
	if req.Method != "GET" && req.Method != "HEAD" {
		w.JSON(405, map[string]any{"error": map[string]any{
			"message": "方法不允许",
			"type":    "invalid_request_error",
			"code":    405,
		}})
		return
	}
	w.writeHead(200, map[string]string{
		"Content-Type":   "text/html; charset=utf-8",
		"Content-Length": strconv.Itoa(len(uiHTML)),
		"Cache-Control":  "no-store",
		"Connection":     "close",
	})
	if req.Method == "GET" {
		_, _ = w.conn.Write([]byte(uiHTML))
	}
}
