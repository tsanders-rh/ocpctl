package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// newCtx builds an Echo context backed by httptest for exercising middleware.
func newCtx(method, target string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// okHandler writes a 200 response.
func okHandler(c echo.Context) error { return c.String(http.StatusOK, "ok") }

func TestCacheControl(t *testing.T) {
	tests := []struct {
		name string
		mw   echo.MiddlewareFunc
		want string
	}{
		{"public ttl", CacheControl(60), "public, max-age=60"},
		{"no-cache ttl 0", CacheControl(0), "no-cache, private"},
		{"no-store ttl -1", CacheControl(-1), "private, no-store, must-revalidate"},
		{"NoCache", NoCache(), "private, no-store, must-revalidate"},
		{"CachePublic", CachePublic(2 * time.Minute), "public, max-age=120"},
		{"CachePrivate", CachePrivate(90 * time.Second), "private, max-age=90"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newCtx(http.MethodGet, "/")
			if err := tt.mw(okHandler)(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCacheControlSkipsOnError(t *testing.T) {
	c, rec := newCtx(http.MethodGet, "/")
	failing := func(c echo.Context) error { return echo.NewHTTPError(http.StatusInternalServerError, "boom") }
	if err := CacheControl(60)(failing)(c); err == nil {
		t.Fatal("expected error to propagate")
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("expected no Cache-Control on error, got %q", got)
	}
}

func TestRateLimiterStore(t *testing.T) {
	s := NewRateLimiterStore(60, 5)
	l1 := s.getLimiter("10.0.0.1")
	l2 := s.getLimiter("10.0.0.1")
	if l1 != l2 {
		t.Error("expected same limiter for same IP")
	}
	if l3 := s.getLimiter("10.0.0.2"); l3 == l1 {
		t.Error("expected distinct limiter for different IP")
	}
}

func TestRateLimitBlocksOverBurst(t *testing.T) {
	mw := RateLimit(60, 1) // burst of 1: first request passes, second is throttled
	handler := mw(okHandler)

	c1, _ := newCtx(http.MethodGet, "/")
	if err := handler(c1); err != nil {
		t.Fatalf("first request should pass, got %v", err)
	}

	c2, _ := newCtx(http.MethodGet, "/")
	err := handler(c2)
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusTooManyRequests {
		t.Fatalf("second request should be rate limited, got %v", err)
	}
}

func TestRateLimitWithMessage(t *testing.T) {
	mw := StrictRateLimitWithMessage(60, "slow down")
	handler := mw(okHandler)

	c1, _ := newCtx(http.MethodGet, "/")
	if err := handler(c1); err != nil {
		t.Fatalf("first request should pass, got %v", err)
	}
	c2, _ := newCtx(http.MethodGet, "/")
	err := handler(c2)
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %v", err)
	}
	if msg, _ := he.Message.(string); !strings.Contains(msg, "slow down") {
		t.Errorf("expected custom message, got %v", he.Message)
	}
}

func TestStrictRateLimit(t *testing.T) {
	handler := StrictRateLimit(60)(okHandler)
	c1, _ := newCtx(http.MethodGet, "/")
	if err := handler(c1); err != nil {
		t.Fatalf("first request should pass, got %v", err)
	}
	c2, _ := newCtx(http.MethodGet, "/")
	if err := handler(c2); err == nil {
		t.Error("expected strict limiter to throttle the second request")
	}
}

func TestPrometheusMetrics(t *testing.T) {
	// Normal request path records metrics and passes through.
	c, _ := newCtx(http.MethodGet, "/api/v1/clusters")
	c.SetPath("/api/v1/clusters")
	if err := PrometheusMetrics()(okHandler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A handler that writes nothing leaves status 0; middleware normalizes to 200.
	c2, _ := newCtx(http.MethodGet, "/noop")
	c2.SetPath("/noop")
	noWrite := func(c echo.Context) error { return nil }
	if err := PrometheusMetrics()(noWrite)(c2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrometheusMetricsSkipsMetricsEndpoint(t *testing.T) {
	c, _ := newCtx(http.MethodGet, "/metrics")
	c.SetPath("/metrics")
	called := false
	handler := func(c echo.Context) error { called = true; return nil }
	if err := PrometheusMetrics()(handler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected next handler to run for /metrics")
	}
}

func TestLogger(t *testing.T) {
	c, _ := newCtx(http.MethodGet, "/")
	if err := Logger()(okHandler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
