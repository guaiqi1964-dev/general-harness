// 用量统计仓库（Go 版）：JSON 文件持久化，零第三方依赖。
// 写入使用互斥锁保护（本引擎为单进程，无独立写线程需求）；
// 与 Python 版 schema 兼容（同名字段），查询接口一致。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// UsageRecord 一条用量记录（字段与 Python 版 usage_records 对齐）。
type UsageRecord struct {
	SessionID        string  `json:"session_id"`
	RequestID        string  `json:"request_id"`
	APIKeyName       string  `json:"api_key_name"`
	ModelName        string  `json:"model_name"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Timestamp        float64 `json:"timestamp"`
}

// UsageStore 用量存储。
type UsageStore struct {
	mu      sync.Mutex
	path    string
	records []UsageRecord
}

func newUsageStore(path string) *UsageStore {
	s := &UsageStore{path: path}
	s.load()
	return s
}

func (s *UsageStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.records)
}

func (s *UsageStore) persist() {
	tmp := s.path + ".tmp"
	dir := filepath.Dir(s.path)
	_ = os.MkdirAll(dir, 0o755)
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// Record 追加一条记录（线程安全，原子持久化）。
func (s *UsageStore) Record(sessionID, requestID, apiKeyName, modelName string,
	promptTokens, completionTokens, totalTokens int, timestamp float64) {
	if timestamp <= 0 {
		timestamp = float64(time.Now().Unix())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, UsageRecord{
		SessionID: sessionID, RequestID: requestID, APIKeyName: apiKeyName,
		ModelName: modelName, PromptTokens: promptTokens,
		CompletionTokens: completionTokens, TotalTokens: totalTokens,
		Timestamp: timestamp,
	})
	s.persist()
}

// ---- 时间维度聚合 ----

var rangeSeconds = map[string]float64{
	"10min": 600, "0.5h": 1800, "1h": 3600, "2h": 7200, "5h": 18000,
	"10h": 36000, "1d": 86400, "7d": 604800, "30d": 2592000, "0.5y": 15778800,
}

var rangeBuckets = map[string]int64{
	"10min": 60, "0.5h": 60, "1h": 300, "2h": 600, "5h": 1800,
	"10h": 3600, "1d": 3600, "7d": 21600, "30d": 86400, "0.5y": 604800,
}

var bucketCandidates = []int64{60, 300, 600, 1800, 3600, 7200, 21600, 43200, 86400, 604800, 1209600, 2592000}

func pickBucket(span float64) int64 {
	if span <= 0 {
		return 86400
	}
	target := span / 50.0
	for _, c := range bucketCandidates {
		if float64(c) >= target {
			return c
		}
	}
	return bucketCandidates[len(bucketCandidates)-1]
}

type timeSeriesItem struct {
	Label   string `json:"label"`
	Tokens  int64  `json:"tokens"`
	StartTS int64  `json:"start_ts"`
	EndTS   int64  `json:"end_ts"`
}

func formatBucketLabel(bucketStart int64, bucket int64) string {
	t := time.Unix(bucketStart, 0).Local()
	if bucket < 3600 {
		return t.Format("15:04")
	}
	if bucket < 86400 {
		return t.Format("01-02 15:04")
	}
	return t.Format("2006-01-02")
}

func (s *UsageStore) QueryTimeSeries(timeRange string, startTS, endTS *float64) ([]timeSeriesItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := float64(time.Now().Unix())
	var start, end float64
	var bucket int64

	if timeRange == "total" {
		if len(s.records) == 0 {
			return []timeSeriesItem{}, nil
		}
		start, end = s.records[0].Timestamp, s.records[0].Timestamp
		for _, r := range s.records {
			if r.Timestamp < start {
				start = r.Timestamp
			}
			if r.Timestamp > end {
				end = r.Timestamp
			}
		}
		bucket = pickBucket(end - start)
	} else if timeRange == "custom" {
		start = 0
		if startTS != nil {
			start = *startTS
		}
		end = now
		if endTS != nil {
			end = *endTS
		}
		if end <= start {
			start, end = end, start
		}
		bucket = pickBucket(end - start)
	} else {
		span, ok := rangeSeconds[timeRange]
		if !ok {
			return nil, &PluginError{Message: "无效的 time_range: " + timeRange, StatusCode: 400, ErrorType: "invalid_request_error", Code: 400}
		}
		bucket = rangeBuckets[timeRange]
		end = now
		start = end - span
	}

	agg := map[int64]int64{}
	for _, r := range s.records {
		if r.Timestamp < start || r.Timestamp > end {
			continue
		}
		bs := int64(r.Timestamp) / bucket * bucket
		agg[bs] += int64(r.TotalTokens)
	}
	first := int64(start) / bucket * bucket
	last := int64(end) / bucket * bucket
	items := []timeSeriesItem{}
	for bs := first; bs <= last; bs += bucket {
		items = append(items, timeSeriesItem{
			Label: formatBucketLabel(bs, bucket), Tokens: agg[bs],
			StartTS: bs, EndTS: bs + bucket,
		})
	}
	return items, nil
}

// ---- 对话维度聚合 ----

type conversationItem struct {
	Label     string  `json:"label"`
	Tokens    int64   `json:"tokens"`
	SessionID string  `json:"session_id"`
	ModelName string  `json:"model_name,omitempty"`
	LastTS    float64 `json:"last_ts"`
}

func (s *UsageStore) QueryRecentConversations(limit int) ([]conversationItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bySession := map[string]*conversationItem{}
	for _, r := range s.records {
		if r.SessionID == "" {
			continue
		}
		item, ok := bySession[r.SessionID]
		if !ok {
			item = &conversationItem{SessionID: r.SessionID, LastTS: r.Timestamp}
			bySession[r.SessionID] = item
		}
		item.Tokens += int64(r.TotalTokens)
		if r.Timestamp > item.LastTS {
			item.LastTS = r.Timestamp
			item.ModelName = r.ModelName
		}
	}
	items := []conversationItem{}
	for _, item := range bySession {
		label := item.SessionID
		if len(label) > 14 {
			label = label[:12] + "…"
		}
		item.Label = label
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastTS > items[j].LastTS })
	if limit >= 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
