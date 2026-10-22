// CLI 交互层测试：命令解析、渲染、用量统计、流式与中断、管道模式。
// 全部离线：厂商调用用 StreamHandler 桩替代，不打网络。
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestCLI 构造输出到内存缓冲的会话（关掉着色，断言稳定）。
func newTestCLI(t *testing.T) (*cliSession, *bytes.Buffer) {
	t.Helper()
	e := testEngine(t)
	var buf bytes.Buffer
	s := newCLISession(e, strings.NewReader(""), &buf)
	s.color = false
	return s, &buf
}

func TestSplitCommandParsesNameAndArgs(t *testing.T) {
	cases := []struct {
		in, name, arg string
	}{
		{"/HELP", "/help", ""},
		{"/key deepseek sk-abc", "/key", "deepseek sk-abc"},
		{"  /model   2  ", "/model", "2"},
		{"/usage", "/usage", ""},
	}
	for _, c := range cases {
		name, arg := splitCommand(c.in)
		if name != c.name || arg != c.arg {
			t.Errorf("splitCommand(%q) = (%q, %q)，期望 (%q, %q)", c.in, name, arg, c.name, c.arg)
		}
	}
}

func TestParseIndexAcceptsDigitsOnly(t *testing.T) {
	if n, ok := parseIndex("12"); !ok || n != 12 {
		t.Errorf("parseIndex(12) = (%d, %v)", n, ok)
	}
	for _, bad := range []string{"", "1a", "-1", " 2", "1.5"} {
		if _, ok := parseIndex(bad); ok {
			t.Errorf("parseIndex(%q) 应当失败", bad)
		}
	}
}

func TestCLIPromptShowsModelAndWebState(t *testing.T) {
	s, _ := newTestCLI(t)
	s.model = "fake/fake-model"
	if got := s.prompt(); !strings.Contains(got, "fake-model") || !strings.Contains(got, "联网:关") {
		t.Errorf("prompt = %q", got)
	}
	s.webSearch = true
	if got := s.prompt(); !strings.Contains(got, "联网:开") {
		t.Errorf("开启联网后 prompt = %q", got)
	}
	if shortModel("local/demo") != "demo" || shortModel("plain") != "plain" {
		t.Errorf("shortModel 行为不符")
	}
}

func TestCLIUnknownCommandReports(t *testing.T) {
	s, buf := newTestCLI(t)
	handled, quit := s.handleCommand("/nope")
	if !handled || quit {
		t.Fatalf("未知命令应被处理且不退出，得到 handled=%v quit=%v", handled, quit)
	}
	if !strings.Contains(buf.String(), "未知命令") {
		t.Errorf("输出 = %q", buf.String())
	}
	// 普通文本不算命令，交给对话分支。
	if handled, _ := s.handleCommand("你好"); handled {
		t.Errorf("普通文本不应被当作命令")
	}
	// /exit 退出。
	if _, quit := s.handleCommand("/exit"); !quit {
		t.Errorf("/exit 应返回 quit=true")
	}
}

func TestCLIModelsMarksCurrentModel(t *testing.T) {
	s, buf := newTestCLI(t)
	s.model = "fake/fake-model"
	s.cmdModels()
	out := buf.String()
	if !strings.Contains(out, "fake/fake-model") {
		t.Fatalf("模型列表缺 fake/fake-model：%q", out)
	}
	// 当前模型前面有 ▸ 标记。
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "fake/fake-model") && !strings.Contains(line, "▸") {
			t.Errorf("当前模型行没有标记: %q", line)
		}
	}
}

func TestCLIModelSwitchByIndexAndName(t *testing.T) {
	s, buf := newTestCLI(t)
	s.cmdModel("1")
	if s.model != "fake/fake-model" {
		t.Errorf("按序号切换后 model = %q", s.model)
	}
	s.cmdModel("99")
	if !strings.Contains(buf.String(), "超出范围") {
		t.Errorf("越界序号应有提示：%q", buf.String())
	}
	s.model = "fake/fake-model"
	s.cmdModel("nonexistent/model")
	if !strings.Contains(buf.String(), "未知模型") && !strings.Contains(buf.String(), "✗") {
		t.Errorf("未知模型应有错误提示：%q", buf.String())
	}
	if s.model != "fake/fake-model" {
		t.Errorf("切换失败后不应改动当前模型，得到 %q", s.model)
	}
}

func TestCLIThinkToggle(t *testing.T) {
	s, buf := newTestCLI(t)
	if s.showThink {
		t.Fatalf("默认应折叠思考过程")
	}
	s.cmdThink("on")
	if !s.showThink {
		t.Fatalf("/think on 未生效")
	}
	s.cmdThink("off")
	if s.showThink {
		t.Fatalf("/think off 未生效")
	}
	s.cmdThink("bogus")
	if !strings.Contains(buf.String(), "用法") {
		t.Errorf("非法参数应给用法提示：%q", buf.String())
	}
}

func TestCLIHistoryAndClear(t *testing.T) {
	s, buf := newTestCLI(t)
	s.cmdHistory()
	if !strings.Contains(buf.String(), "还没有对话") {
		t.Errorf("空历史提示缺失：%q", buf.String())
	}
	s.history = []map[string]any{
		{"role": "user", "content": "第一个问题"},
		{"role": "assistant", "content": "第一个回答"},
	}
	buf.Reset()
	s.cmdHistory()
	if !strings.Contains(buf.String(), "1 轮") || !strings.Contains(buf.String(), "第一个问题") {
		t.Errorf("历史摘要不符：%q", buf.String())
	}
	s.handleCommand("/clear")
	if len(s.history) != 0 {
		t.Errorf("/clear 后历史应为空，实际 %d", len(s.history))
	}
}

func TestCLIConfigShowsKeyFacts(t *testing.T) {
	s, buf := newTestCLI(t)
	s.cmdConfig()
	out := buf.String()
	for _, want := range []string{"监听", "限流", "默认模型", "联网检索", "密钥文件", "用量文件", "会话 ID"} {
		if !strings.Contains(out, want) {
			t.Errorf("/config 输出缺少 %q：\n%s", want, out)
		}
	}
	if !strings.Contains(out, s.model) {
		t.Errorf("/config 未显示默认模型：\n%s", out)
	}
}

func TestCLIUsageTimeSeriesRendersBar(t *testing.T) {
	s, buf := newTestCLI(t)
	s.engine.Usage.Record("sess-a", "req-1", "default", "fake-model", 10, 20, 30, 0)
	s.engine.Usage.Record("sess-b", "req-2", "default", "fake-model", 100, 200, 300, 0)
	s.cmdUsage("1d")
	out := buf.String()
	if !strings.Contains(out, "合计 330 tokens") {
		t.Errorf("合计不符：\n%s", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("缺少条形图：\n%s", out)
	}
	if !strings.Contains(out, "/usage conv") {
		t.Errorf("缺少会话明细提示：\n%s", out)
	}
	if bar := usageBar(0, 10, 10); bar != "" {
		t.Errorf("0 值不应画条形图，得到 %q", bar)
	}
	// 非零但极小的值至少画一格，否则看起来像没有数据。
	if bar := usageBar(1, 1000, 10); bar != "█" {
		t.Errorf("极小值应至少一格，得到 %q", bar)
	}
}

func TestCLIUsageConversationsList(t *testing.T) {
	s, buf := newTestCLI(t)
	s.engine.Usage.Record("sess-cli-1", "req-1", "default", "fake-model", 1, 2, 3, 0)
	s.cmdUsage("conv")
	out := buf.String()
	if !strings.Contains(out, "sess-cli-1") || !strings.Contains(out, "tokens") {
		t.Errorf("会话列表不符：\n%s", out)
	}
	buf.Reset()
	s.cmdUsage("bogus")
	if !strings.Contains(buf.String(), "用法") {
		t.Errorf("非法区间应给用法提示：%q", buf.String())
	}
}

func TestCLIKeyListAndClear(t *testing.T) {
	s, buf := newTestCLI(t)
	s.cmdKey("")
	out := buf.String()
	if !strings.Contains(out, "fake") || !strings.Contains(out, "Key") {
		t.Fatalf("Key 列表不符：\n%s", out)
	}
	// 设置覆盖
	buf.Reset()
	s.cmdKey("fake sk-cli-test-123456")
	if !strings.Contains(buf.String(), "已保存") {
		t.Fatalf("设置 Key 失败：%q", buf.String())
	}
	if _, err := os.Stat(s.engine.KeysPath); err != nil {
		t.Fatalf("keys.yaml 未落盘: %v", err)
	}
	// 覆盖生效
	s.engine.keysMod = time.Time{}
	s.engine.refreshKeysIfChanged()
	if k := s.engine.Cloud.Providers["fake"].defaultKey().Key; k != "sk-cli-test-123456" {
		t.Errorf("覆盖未生效，当前 Key = %q", k)
	}
	// 清除覆盖
	buf.Reset()
	s.cmdKey("fake -")
	if !strings.Contains(buf.String(), "已清除") {
		t.Fatalf("清除覆盖失败：%q", buf.String())
	}
	s.engine.keysMod = time.Time{}
	s.engine.refreshKeysIfChanged()
	if k := s.engine.Cloud.Providers["fake"].defaultKey().Key; k == "sk-cli-test-123456" {
		t.Errorf("清除后仍在使用覆盖 Key")
	}
	// 未知厂商
	buf.Reset()
	s.cmdKey("nope sk-x")
	if !strings.Contains(buf.String(), "未知厂商") {
		t.Errorf("未知厂商应报错：%q", buf.String())
	}
}

// /key list 与 /key help 是列清单的别名，不能被当成密钥去联网探测。
func TestCLIKeyListAlias(t *testing.T) {
	for _, alias := range []string{"list", "help"} {
		s, buf := newTestCLI(t)
		if _, quit := s.handleCommand("/key " + alias); quit {
			t.Fatalf("/key %s 不应退出", alias)
		}
		if !strings.Contains(buf.String(), "Key") {
			t.Errorf("/key %s 输出 = %q", alias, buf.String())
		}
	}
}

// 单参数不是密钥时只提示用法，不应发起网络探测。
func TestCLIKeySingleArgTooShort(t *testing.T) {
	s, buf := newTestCLI(t)
	s.cmdKey("short")
	out := buf.String()
	if !strings.Contains(out, "不像一把密钥") && !strings.Contains(out, "用法") {
		t.Fatalf("过短的单参数应提示用法：%q", out)
	}
	if strings.Contains(out, "正在探测") {
		t.Errorf("过短的输入不该触发探测：%q", out)
	}
}

func TestCLIKeyListMarksPlaceholder(t *testing.T) {
	s, buf := newTestCLI(t)
	p := s.engine.Cloud.Providers["fake"]
	p.Keys = []APIKey{{Name: "default", Key: "sk-xxxxxx"}}
	s.cmdKey("")
	if !strings.Contains(buf.String(), "占位符") {
		t.Errorf("占位符 Key 应被标注：\n%s", buf.String())
	}
}

func TestCLISearchBackendListShowsReadiness(t *testing.T) {
	e := testEngine(t)
	sc := defaultSearchConfig()
	sc.Enabled = true
	// defaultSearchConfig 的 Backends 是空 map，这里显式放一个就绪后端 + 一个不可达后端。
	sc.Backend = "searxng"
	sc.Backends["searxng"] = &SearchBackend{
		Name: "searxng", Type: "http_json", BaseURL: "http://127.0.0.1:8899",
		Path: "/search", Method: "GET", QueryParam: "q", ResultPath: "results",
	}
	// ready() 只校验配置完整性（地址/路径/环境变量），不探活网络，
	// 所以这里用「引用了未设置的环境变量」来构造一个不可用后端。
	sc.Backends["broken"] = &SearchBackend{
		Name: "broken", Type: "http_json", BaseURL: "http://127.0.0.1:1",
		Path: "/search", Method: "GET", QueryParam: "q", ResultPath: "results",
		Headers: map[string]string{"Authorization": "Bearer ${CLI_TEST_MISSING_ENV}"},
	}
	e.Search = newSearchService(sc)
	var buf bytes.Buffer
	s := newCLISession(e, strings.NewReader(""), &buf)
	s.color = false
	s.cmdSearch("list")
	out := buf.String()
	if !strings.Contains(out, "searxng") || !strings.Contains(out, "broken") {
		t.Fatalf("后端列表不符：\n%s", out)
	}
	if !strings.Contains(out, "不可用") {
		t.Errorf("不可用后端应标注原因：\n%s", out)
	}
	// 切到不可用后端应被拒绝。
	buf.Reset()
	s.cmdSearch("backend broken")
	if !strings.Contains(buf.String(), "不可用") {
		t.Errorf("切到不可用后端应失败：%q", buf.String())
	}
	// 未知后端
	buf.Reset()
	s.cmdSearch("backend nope")
	if !strings.Contains(buf.String(), "未知的搜索后端") {
		t.Errorf("未知后端应报错：%q", buf.String())
	}
}

// stubStream 给厂商装上流式桩：先两段思考，再三段正文。
func stubStream(p *Provider, chunks []map[string]any) {
	p.StreamHandler = func(_ *Provider, _ string, _ []map[string]any, _ string,
		_ *float64, _ *int, _ *float64, _ map[string]any,
		onChunk func(map[string]any) error) error {
		for _, c := range chunks {
			if err := onChunk(c); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestCLIStreamReplyRendersThinkingAndBody(t *testing.T) {
	s, buf := newTestCLI(t)
	s.model = "fake/fake-model"
	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{
		{"thinking": "先想一下，"},
		{"thinking": "再想一下。"},
		{"content": "你好"},
		{"content": "，世界"},
		{"content": "！", "usage": map[string]any{
			"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}},
	})
	reply, sources := s.streamReply([]map[string]any{{"role": "user", "content": "打个招呼"}})
	if reply != "你好，世界！" {
		t.Fatalf("正文 = %q", reply)
	}
	if sources != nil {
		t.Errorf("未开启联网时不应有来源")
	}
	out := buf.String()
	// 折叠模式：思考只显示字数，不出现思考原文。
	if strings.Contains(out, "先想一下") {
		t.Errorf("默认应折叠思考内容，输出:\n%s", out)
	}
	if !strings.Contains(out, "思考") || !strings.Contains(out, "字") {
		t.Errorf("折叠提示缺失：\n%s", out)
	}
	// 非终端下只给一行汇总，不应把逐帧刷新序列灌进输出。
	if strings.Contains(out, "\r") {
		t.Errorf("非终端输出不应包含回车刷新：%q", out)
	}
	if !strings.Contains(out, "⏱") {
		t.Errorf("缺少耗时统计：\n%s", out)
	}
	// usage 已记账。
	items, err := s.engine.Usage.QueryRecentConversations(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[0].Tokens != 8 {
		t.Errorf("用量未记录，items=%+v", items)
	}
}

func TestCLIStreamReplyExpandsThinkingWhenEnabled(t *testing.T) {
	s, buf := newTestCLI(t)
	s.model = "fake/fake-model"
	s.showThink = true
	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{
		{"thinking": "推理过程"},
		{"content": "答案"},
	})
	reply, _ := s.streamReply([]map[string]any{{"role": "user", "content": "问"}})
	if reply != "答案" {
		t.Errorf("正文 = %q", reply)
	}
	if !strings.Contains(buf.String(), "推理过程") {
		t.Errorf("/think on 后应展开思考内容：\n%s", buf.String())
	}
}

func TestCLIStreamReplyCanceledStopsEarly(t *testing.T) {
	s, buf := newTestCLI(t)
	s.model = "fake/fake-model"
	p := s.engine.Cloud.Providers["fake"]
	// 第二块时就模拟用户按下 Ctrl+C。
	p.StreamHandler = func(_ *Provider, _ string, _ []map[string]any, _ string,
		_ *float64, _ *int, _ *float64, _ map[string]any,
		onChunk func(map[string]any) error) error {
		if err := onChunk(map[string]any{"content": "第一段"}); err != nil {
			return err
		}
		s.cancel = 1
		return onChunk(map[string]any{"content": "第二段"})
	}
	reply, _ := s.streamReply([]map[string]any{{"role": "user", "content": "问"}})
	if reply != "第一段" {
		t.Errorf("中断后应只保留已输出内容，得到 %q", reply)
	}
	if !strings.Contains(buf.String(), "已中断") {
		t.Errorf("缺少中断提示：\n%s", buf.String())
	}
	// 中断后 streaming 标志必须复位，否则下一次 Ctrl+C 会误判。
	if s.streaming != 0 {
		t.Errorf("streaming 标志未复位")
	}
}

func TestCLIStreamReplyReportsUpstreamError(t *testing.T) {
	s, buf := newTestCLI(t)
	s.model = "fake/fake-model"
	s.engine.Cloud.Providers["fake"].StreamHandler = func(_ *Provider, _ string, _ []map[string]any, _ string,
		_ *float64, _ *int, _ *float64, _ map[string]any,
		_ func(map[string]any) error) error {
		return newPluginError("上游连接错误，请稍后重试", 502, "upstream_error")
	}
	reply, _ := s.streamReply([]map[string]any{{"role": "user", "content": "问"}})
	if reply != "" {
		t.Errorf("失败时不应有正文，得到 %q", reply)
	}
	out := buf.String()
	if !strings.Contains(out, "上游连接错误") {
		t.Errorf("应显示上游错误：\n%s", out)
	}
	if !strings.Contains(out, "网络") && !strings.Contains(out, "提示") {
		t.Errorf("应给出排查提示：\n%s", out)
	}
}

func TestCLIPipeModeSendsPromptAndPrintsAnswer(t *testing.T) {
	e := testEngine(t)
	p := e.Cloud.Providers["fake"]
	var gotUser string
	p.StreamHandler = func(_ *Provider, _ string, messages []map[string]any, _ string,
		_ *float64, _ *int, _ *float64, _ map[string]any,
		onChunk func(map[string]any) error) error {
		if len(messages) > 0 {
			gotUser = toStr(messages[len(messages)-1]["content"])
		}
		return onChunk(map[string]any{"content": "管道回答"})
	}
	var out, errOut bytes.Buffer
	code := runPipe(e, strings.NewReader("标准输入里的问题\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut.String())
	}
	if gotUser != "标准输入里的问题" {
		t.Errorf("管道输入未作为用户消息，得到 %q", gotUser)
	}
	if !strings.Contains(out.String(), "管道回答") {
		t.Errorf("输出缺回答：%q", out.String())
	}
	// 空输入应报错退出。
	out.Reset()
	if code := runPipe(e, strings.NewReader("   \n"), &out, &errOut); code == 0 {
		t.Errorf("空输入应返回非 0")
	}
	if !strings.Contains(errOut.String(), "标准输入为空") {
		t.Errorf("空输入提示缺失：%q", errOut.String())
	}
}

func TestCLIPipeModeFailsWhenUpstreamErrors(t *testing.T) {
	e := testEngine(t)
	e.Cloud.Providers["fake"].StreamHandler = func(_ *Provider, _ string, _ []map[string]any, _ string,
		_ *float64, _ *int, _ *float64, _ map[string]any,
		_ func(map[string]any) error) error {
		return newPluginError("厂商 fake 未配置有效的 api_key / api_keys", 401, "authentication_error")
	}
	var out, errOut bytes.Buffer
	if code := runPipe(e, strings.NewReader("问题"), &out, &errOut); code == 0 {
		t.Errorf("上游失败时管道模式应返回非 0")
	}
	if !strings.Contains(out.String(), "api_key") {
		t.Errorf("应显示鉴权错误：%q", out.String())
	}
}

func TestCLIChatKeepsHistoryOnSuccessOnly(t *testing.T) {
	s, _ := newTestCLI(t)
	s.model = "fake/fake-model"
	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{{"content": "好的"}})
	s.chat("问题一")
	if len(s.history) != 2 {
		t.Fatalf("成功一轮后应有 2 条消息，得到 %d", len(s.history))
	}
	// 失败时不应把空回答写进历史。
	s.engine.Cloud.Providers["fake"].StreamHandler = func(_ *Provider, _ string, _ []map[string]any, _ string,
		_ *float64, _ *int, _ *float64, _ map[string]any,
		_ func(map[string]any) error) error {
		return newPluginError("boom", 500, "internal_error")
	}
	s.chat("问题二")
	if len(s.history) != 2 {
		t.Errorf("失败后历史不应增长，得到 %d 条", len(s.history))
	}
}

func TestCLIKeysFileIsUnderEngineRoot(t *testing.T) {
	s, _ := newTestCLI(t)
	want := filepath.Join(filepath.Dir(s.engine.KeysPath), "keys.yaml")
	if s.engine.KeysPath != want {
		t.Errorf("KeysPath = %q，期望 %q", s.engine.KeysPath, want)
	}
	if s.engine.KeysPath == "" || !strings.HasSuffix(s.engine.KeysPath, "keys.yaml") {
		t.Errorf("KeysPath 不合理: %q", s.engine.KeysPath)
	}
}

// 重新生成：丢弃最后一轮问答、用同一个问题重发，上下文里只留一份提问。
func TestCLIRegenReplacesLastAnswer(t *testing.T) {
	s, _ := newTestCLI(t)
	s.model = "fake/fake-model"
	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{
		{"content": "第一版回答"},
	})
	s.chat("你好")
	if len(s.history) != 2 {
		t.Fatalf("首轮后 history 长度 = %d，期望 2", len(s.history))
	}

	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{
		{"content": "重写后的回答"},
	})
	handled, quit := s.handleCommand("/regen")
	if !handled || quit {
		t.Fatalf("handleCommand(/regen) = (%v, %v)", handled, quit)
	}
	if len(s.history) != 2 {
		t.Fatalf("重新生成后 history 长度 = %d，期望 2（旧回答应被替换）", len(s.history))
	}
	if q := toStr(s.history[0]["content"]); q != "你好" {
		t.Errorf("提问 = %q，期望原问题", q)
	}
	if a := toStr(s.history[1]["content"]); a != "重写后的回答" {
		t.Errorf("回答 = %q，期望被替换", a)
	}
}

// 上下文里有两轮问答时，重新生成只丢最后一轮，前面保留。
func TestCLIRegenKeepsEarlierTurns(t *testing.T) {
	s, _ := newTestCLI(t)
	s.model = "fake/fake-model"
	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{{"content": "A1"}})
	s.chat("Q1")
	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{{"content": "A2"}})
	s.chat("Q2")
	if len(s.history) != 4 {
		t.Fatalf("两轮后 history 长度 = %d，期望 4", len(s.history))
	}

	stubStream(s.engine.Cloud.Providers["fake"], []map[string]any{{"content": "A2-新"}})
	s.cmdRegen()
	if len(s.history) != 4 {
		t.Fatalf("重新生成后 history 长度 = %d，期望 4", len(s.history))
	}
	if toStr(s.history[0]["content"]) != "Q1" || toStr(s.history[1]["content"]) != "A1" {
		t.Errorf("第一轮被误删: %v", s.history[:2])
	}
	if toStr(s.history[3]["content"]) != "A2-新" {
		t.Errorf("最后一轮未被替换: %q", toStr(s.history[3]["content"]))
	}
}

// 空对话时给出明确提示，不抛错、不污染 history。
func TestCLIRegenOnEmptyHistory(t *testing.T) {
	s, buf := newTestCLI(t)
	handled, quit := s.handleCommand("/regen")
	if !handled || quit {
		t.Fatalf("handleCommand(/regen) = (%v, %v)", handled, quit)
	}
	if len(s.history) != 0 {
		t.Errorf("空对话时不应写入 history")
	}
	if !strings.Contains(buf.String(), "没有可重新生成") {
		t.Errorf("缺少提示，输出:\n%s", buf.String())
	}
}
