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

func TestValidateChirp(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr error
	}{
		{"short chirp", "This is an opinion I need to share with the world", "This is an opinion I need to share with the world", nil},
		{"exactly 140 chars", strings.Repeat("a", 140), strings.Repeat("a", 140), nil},
		{"141 chars", strings.Repeat("a", 141), "", errChirpTooLong},
		{"140 multibyte chars", strings.Repeat("é", 140), strings.Repeat("é", 140), nil},
		{"empty body", "", "", nil},
		{"profane word", "This is a kerfuffle opinion I need to share with the world", "This is a **** opinion I need to share with the world", nil},
		{"mixed case and multiple", "I hear Mastodon is better than Chirpy. SHARBERT Fornax kerfuffle", "I hear Mastodon is better than Chirpy. **** **** ****", nil},
		{"punctuation not replaced", "Sharbert! is not kerfuffle.", "Sharbert! is not kerfuffle.", nil},
		{"profane word in long chirp still rejected", "kerfuffle " + strings.Repeat("a", 140), "", errChirpTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateChirp(tt.body)
			if err != tt.wantErr {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("cleaned = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHandlerCreateChirpRejectsInvalid covers requests rejected before any
// database access, so the config has no db.
func TestHandlerCreateChirpRejectsInvalid(t *testing.T) {
	const userID = "123e4567-e89b-12d3-a456-426614174000"
	tests := []struct {
		name     string
		body     string
		wantJSON string
	}{
		{"invalid JSON", `not json`, `{"error":"Something went wrong"}`},
		{"empty request body", ``, `{"error":"Something went wrong"}`},
		{"missing user_id", `{"body":"hi"}`, `{"error":"Invalid user_id"}`},
		{"malformed user_id", `{"body":"hi","user_id":"nope"}`, `{"error":"Invalid user_id"}`},
		{"too long", `{"body":"` + strings.Repeat("a", 141) + `","user_id":"` + userID + `"}`, `{"error":"Chirp is too long"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &apiConfig{}
			rec := httptest.NewRecorder()
			cfg.handlerCreateChirp(rec, httptest.NewRequest(http.MethodPost, "/api/chirps", strings.NewReader(tt.body)))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if got := rec.Body.String(); got != tt.wantJSON {
				t.Errorf("body = %q, want %q", got, tt.wantJSON)
			}
		})
	}
}

func TestHandlerGetChirpInvalidID(t *testing.T) {
	cfg := &apiConfig{}
	req := httptest.NewRequest(http.MethodGet, "/api/chirps/nope", nil)
	req.SetPathValue("chirpID", "nope")
	rec := httptest.NewRecorder()
	cfg.handlerGetChirp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got, want := rec.Body.String(), `{"error":"Invalid chirp ID"}`; got != want {
		t.Errorf("body = %q, want %q", got, want)
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
