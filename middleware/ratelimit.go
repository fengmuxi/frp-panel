package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ratelimit.go 固定窗口限流器（安全审计 P1-5）：
// 登录/注册/cert/frps 插件端点此前零限流，可无限在线爆破。
// 内存态实现（master 单进程），按 key（ClientIP 或 "user:"+账号）计数。

// RateLimiter 固定窗口 + 封禁的限流器。
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	window  time.Duration // 计数窗口
	max     int           // 窗口内允许次数
	lockout time.Duration // 超限后的封禁时长
}

type rateEntry struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

// NewRateLimiter 创建限流器：window 内超过 max 次访问后，key 被封禁 lockout 时长。
func NewRateLimiter(window time.Duration, max int, lockout time.Duration) *RateLimiter {
	return &RateLimiter{
		entries: map[string]*rateEntry{},
		window:  window,
		max:     max,
		lockout: lockout,
	}
}

// Allow 记一次访问并返回是否放行。
func (r *RateLimiter) Allow(key string) bool {
	if key == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()

	e, ok := r.entries[key]
	if !ok || now.Sub(e.windowStart) >= r.window {
		var blockedUntil time.Time
		if ok {
			blockedUntil = e.blockedUntil
		}
		e = &rateEntry{windowStart: now, blockedUntil: blockedUntil}
		r.entries[key] = e
	}

	if now.Before(e.blockedUntil) {
		return false
	}
	e.count++
	if e.count > r.max {
		e.blockedUntil = now.Add(r.lockout)
		return false
	}
	return true
}

// Middleware 按 ClientIP 限流的 gin 中间件；超限返回 429。
func (r *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !r.Allow(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, map[string]interface{}{
				"code":    http.StatusTooManyRequests,
				"message": "too many requests, try again later",
			})
			return
		}
		c.Next()
	}
}
