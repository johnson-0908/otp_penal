package auth

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type IPLimiter struct {
	mu       sync.Mutex
	limits   map[string]*rate.Limiter
	rps      rate.Limit
	burst    int
	lastSeen map[string]time.Time
}

func NewIPLimiter(rps rate.Limit, burst int) *IPLimiter {
	return &IPLimiter{
		limits:   make(map[string]*rate.Limiter),
		lastSeen: make(map[string]time.Time),
		rps:      rps,
		burst:    burst,
	}
}

func (l *IPLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.limits[ip]
	if !ok {
		lim = rate.NewLimiter(l.rps, l.burst)
		l.limits[ip] = lim
	}
	l.lastSeen[ip] = time.Now()
	l.gc()
	return lim.Allow()
}

func (l *IPLimiter) gc() {
	if len(l.limits) < 4096 {
		return
	}
	cutoff := time.Now().Add(-1 * time.Hour)
	for ip, t := range l.lastSeen {
		if t.Before(cutoff) {
			delete(l.limits, ip)
			delete(l.lastSeen, ip)
		}
	}
}

func ClientIP(remoteAddr, xff, xri string) string {
	if xri != "" {
		return xri
	}
	if xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return trimSpaces(xff[:i])
			}
		}
		return trimSpaces(xff)
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func trimSpaces(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
