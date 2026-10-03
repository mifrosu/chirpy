package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareMetricsInc(t *testing.T) {
	cfg := &apiConfig{}
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusTeapot)
	})
	h := cfg.middlewareMetricsInc(next)

	for i := 1; i <= 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/", nil))

		if rec.Code != http.StatusTeapot {
			t.Errorf("request %d: status = %d, want %d (next handler's response)", i, rec.Code, http.StatusTeapot)
		}
		if got := cfg.fileserverHits.Load(); got != int32(i) {
			t.Errorf("request %d: hits = %d, want %d", i, got, i)
		}
	}
	if called != 3 {
		t.Errorf("next handler called %d times, want 3", called)
	}
}

func TestHandlerMetrics(t *testing.T) {
	tests := []struct {
		name string
		hits int32
		want string
	}{
		{"zero hits", 0, "Chirpy has been visited 0 times!"},
		{"some hits", 42, "Chirpy has been visited 42 times!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &apiConfig{}
			cfg.fileserverHits.Store(tt.hits)

			rec := httptest.NewRecorder()
			cfg.handlerMetrics(rec, httptest.NewRequest(http.MethodGet, "/admin/metrics", nil))

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got, want := rec.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}
			body := rec.Body.String()
			for _, want := range []string{"<html>", "<h1>Welcome, Chirpy Admin</h1>", tt.want} {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q:\n%s", want, body)
				}
			}
		})
	}
}

func TestHandlerMetricsDoesNotIncrement(t *testing.T) {
	cfg := &apiConfig{}
	for range 3 {
		cfg.handlerMetrics(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/metrics", nil))
	}
	if got := cfg.fileserverHits.Load(); got != 0 {
		t.Errorf("hits = %d after reading metrics, want 0", got)
	}
}

func TestHandlerReset(t *testing.T) {
	cfg := &apiConfig{}
	cfg.fileserverHits.Store(7)

	rec := httptest.NewRecorder()
	cfg.handlerReset(rec, httptest.NewRequest(http.MethodPost, "/api/reset", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := cfg.fileserverHits.Load(); got != 0 {
		t.Errorf("hits = %d after reset, want 0", got)
	}
}

func TestHandlerReadiness(t *testing.T) {
	rec := httptest.NewRecorder()
	handlerReadiness(rec, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := rec.Body.String(), "OK\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
