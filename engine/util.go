// 工具函数与限流器。
package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 熵源异常时用纳秒时间戳兜底，避免全零碰撞
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (8 * (i % 8)))
		}
	}
	return hex.EncodeToString(b)
}

// RateLimiter 滑动窗口限流（单 IP）。
type RateLimiter struct {
	mu        sync.Mutex
	hits      map[string]*hitWindow
	lastPrune float64
}

type hitWindow struct {
	times []float64
}

func newRateLimiter() *RateLimiter {
	return &RateLimiter{hits: map[string]*hitWindow{}}
}

func (r *RateLimiter) Allow(ip string, limit int, windowSec float64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := float64(time.Now().Unix())
	r.maybePrune(now, windowSec)
	h := r.hits[ip]
	if h == nil {
		h = &hitWindow{}
	}
	for len(h.times) > 0 && now-h.times[0] > windowSec {
		h.times = h.times[1:]
	}
	if len(h.times) >= limit {
		r.hits[ip] = h
		return false
	}
	h.times = append(h.times, now)
	r.hits[ip] = h
	return true
}

// maybePrune 周期性清理过期条目，防止 map 无限增长（内存泄漏）。
func (r *RateLimiter) maybePrune(now, windowSec float64) {
	if now-r.lastPrune < 60 {
		return
	}
	r.lastPrune = now
	for ip, h := range r.hits {
		for len(h.times) > 0 && now-h.times[0] > windowSec {
			h.times = h.times[1:]
		}
		if len(h.times) == 0 {
			delete(r.hits, ip)
		}
	}
}
