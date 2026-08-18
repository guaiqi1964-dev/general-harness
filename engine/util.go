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
		return "000000000000"
	}
	return hex.EncodeToString(b)
}

// RateLimiter 滑动窗口限流（单 IP）。
type RateLimiter struct {
	mu   sync.Mutex
	hits map[string]*hitWindow
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
	h := r.hits[ip]
	if h == nil {
		h = &hitWindow{}
		r.hits[ip] = h
	}
	for len(h.times) > 0 && now-h.times[0] > windowSec {
		h.times = h.times[1:]
	}
	if len(h.times) == 0 {
		delete(r.hits, ip)
		h = &hitWindow{}
		r.hits[ip] = h
	}
	if len(h.times) >= limit {
		return false
	}
	h.times = append(h.times, now)
	return true
}
