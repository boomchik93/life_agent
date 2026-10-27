package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

func (s *Server) auth(next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(s.token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		// Hash both sides so the comparison is constant-time regardless of length.
		if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type limiter struct {
	mu      sync.Mutex
	r       rate.Limit
	burst   int
	clients map[string]*client
}

type client struct {
	lim  *rate.Limiter
	seen time.Time
}

func newLimiter(perSec float64, burst int) *limiter {
	return &limiter{r: rate.Limit(perSec), burst: burst, clients: map[string]*client{}}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.clients) > 1024 {
		for k, c := range l.clients {
			if now.Sub(c.seen) > time.Minute {
				delete(l.clients, k)
			}
		}
	}
	c, ok := l.clients[key]
	if !ok {
		c = &client{lim: rate.NewLimiter(l.r, l.burst)}
		l.clients[key] = c
	}
	c.seen = now
	return c.lim.AllowN(now, 1)
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limit.allow(clientIP(r), time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP trusts X-Real-IP only when the request comes from the local nginx.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if real := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); real != nil {
			return real.String()
		}
	}
	return host
}
