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

func TestHandlerResetForbiddenOutsideDev(t *testing.T) {
	cfg := &apiConfig{platform: "prod"}
	cfg.fileserverHits.Store(7)

	rec := httptest.NewRecorder()
	cfg.handlerReset(rec, httptest.NewRequest(http.MethodPost, "/admin/reset", nil))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if got := cfg.fileserverHits.Load(); got != 7 {
		t.Errorf("hits = %d after forbidden reset, want 7", got)
	}
}

func TestHandlerValidateChirp(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantJSON   string
	}{
		{"short chirp", `{"body":"This is an opinion I need to share with the world"}`, http.StatusOK, `{"cleaned_body":"This is an opinion I need to share with the world"}`},
		{"exactly 140 chars", `{"body":"` + strings.Repeat("a", 140) + `"}`, http.StatusOK, `{"cleaned_body":"` + strings.Repeat("a", 140) + `"}`},
		{"141 chars", `{"body":"` + strings.Repeat("a", 141) + `"}`, http.StatusBadRequest, `{"error":"Chirp is too long"}`},
		{"140 multibyte chars", `{"body":"` + strings.Repeat("é", 140) + `"}`, http.StatusOK, `{"cleaned_body":"` + strings.Repeat("é", 140) + `"}`},
		{"empty body field", `{"body":""}`, http.StatusOK, `{"cleaned_body":""}`},
		{"profane word", `{"body":"This is a kerfuffle opinion I need to share with the world"}`, http.StatusOK, `{"cleaned_body":"This is a **** opinion I need to share with the world"}`},
		{"mixed case and multiple", `{"body":"I hear Mastodon is better than Chirpy. SHARBERT Fornax kerfuffle"}`, http.StatusOK, `{"cleaned_body":"I hear Mastodon is better than Chirpy. **** **** ****"}`},
		{"punctuation not replaced", `{"body":"Sharbert! is not kerfuffle."}`, http.StatusOK, `{"cleaned_body":"Sharbert! is not kerfuffle."}`},
		{"profane word in long chirp still rejected", `{"body":"kerfuffle ` + strings.Repeat("a", 140) + `"}`, http.StatusBadRequest, `{"error":"Chirp is too long"}`},
		{"invalid JSON", `not json`, http.StatusBadRequest, `{"error":"Something went wrong"}`},
		{"empty request body", ``, http.StatusBadRequest, `{"error":"Something went wrong"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/validate_chirp", strings.NewReader(tt.body))
			handlerValidateChirp(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got, want := rec.Header().Get("Content-Type"), "application/json"; got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}
			if got := rec.Body.String(); got != tt.wantJSON {
				t.Errorf("body = %q, want %q", got, tt.wantJSON)
			}
		})
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
