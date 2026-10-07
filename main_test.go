package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/scruffling/chirpy/internal/auth"
	"github.com/scruffling/chirpy/internal/database"
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
	const secret = "s3cret"
	userID := uuid.New()
	token, err := auth.MakeJWT(userID, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		body     string
		wantJSON string
	}{
		{"invalid JSON", `not json`, `{"error":"Something went wrong"}`},
		{"empty request body", ``, `{"error":"Something went wrong"}`},
		{"malformed user_id", `{"body":"hi","user_id":"nope"}`, `{"error":"Invalid user_id"}`},
		{"too long", `{"body":"` + strings.Repeat("a", 141) + `","user_id":"` + userID.String() + `"}`, `{"error":"Chirp is too long"}`},
		{"too long without user_id", `{"body":"` + strings.Repeat("a", 141) + `"}`, `{"error":"Chirp is too long"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &apiConfig{jwtSecret: secret}
			req := httptest.NewRequest(http.MethodPost, "/api/chirps", strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			cfg.handlerCreateChirp(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if got := rec.Body.String(); got != tt.wantJSON {
				t.Errorf("body = %q, want %q", got, tt.wantJSON)
			}
		})
	}
}

func TestHandlerCreateChirpAuth(t *testing.T) {
	const secret = "s3cret"
	userID := uuid.New()
	valid, _ := auth.MakeJWT(userID, secret, time.Hour)
	expired, _ := auth.MakeJWT(userID, secret, -time.Minute)
	wrongSecret, _ := auth.MakeJWT(userID, "other", time.Hour)
	body := `{"body":"hi","user_id":"` + userID.String() + `"}`
	otherUser := `{"body":"hi","user_id":"` + uuid.New().String() + `"}`

	tests := []struct {
		name       string
		authHeader string
		body       string
		wantCode   int
		wantJSON   string
	}{
		{"no header", "", body, http.StatusUnauthorized, `{"error":"Unauthorized"}`},
		{"not bearer", "Basic abc", body, http.StatusUnauthorized, `{"error":"Unauthorized"}`},
		{"garbage token", "Bearer nope", body, http.StatusUnauthorized, `{"error":"Unauthorized"}`},
		{"expired token", "Bearer " + expired, body, http.StatusUnauthorized, `{"error":"Unauthorized"}`},
		{"wrong secret", "Bearer " + wrongSecret, body, http.StatusUnauthorized, `{"error":"Unauthorized"}`},
		{"user_id of another user", "Bearer " + valid, otherUser, http.StatusForbidden, `{"error":"Cannot post a chirp as another user"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &apiConfig{jwtSecret: secret}
			req := httptest.NewRequest(http.MethodPost, "/api/chirps", strings.NewReader(tt.body))
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			cfg.handlerCreateChirp(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
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

func TestHandlerLoginRejectsMissingFields(t *testing.T) {
	cfg := &apiConfig{}
	for _, body := range []string{`{}`, `{"email":"a@b.c"}`, `{"password":"x"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
		rec := httptest.NewRecorder()
		cfg.handlerLogin(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want %d", body, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestHandlerRefreshRejectsMissingBearer(t *testing.T) {
	cfg := &apiConfig{}
	for _, header := range []string{"", "Basic abc", "Bearer"} {
		req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		cfg.handlerRefresh(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status = %d, want %d", header, rec.Code, http.StatusUnauthorized)
		}
		if got, want := rec.Body.String(), `{"error":"Unauthorized"}`; got != want {
			t.Errorf("header %q: body = %q, want %q", header, got, want)
		}
	}
}

func TestHandlerRevokeRejectsMissingBearer(t *testing.T) {
	cfg := &apiConfig{}
	for _, header := range []string{"", "Basic abc", "Bearer"} {
		req := httptest.NewRequest(http.MethodPost, "/api/revoke", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		cfg.handlerRevoke(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status = %d, want %d", header, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestHandlerUpdateUserAuth(t *testing.T) {
	const secret = "s3cret"
	userID := uuid.New()
	expired, _ := auth.MakeJWT(userID, secret, -time.Minute)
	wrongSecret, _ := auth.MakeJWT(userID, "other", time.Hour)
	body := `{"email":"a@b.c","password":"pw"}`

	for name, header := range map[string]string{
		"no header":    "",
		"not bearer":   "Basic abc",
		"malformed":    "Bearer nope",
		"expired":      "Bearer " + expired,
		"wrong secret": "Bearer " + wrongSecret,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &apiConfig{jwtSecret: secret}
			req := httptest.NewRequest(http.MethodPut, "/api/users", strings.NewReader(body))
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()
			cfg.handlerUpdateUser(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestHandlerUpdateUserRejectsInvalidBody(t *testing.T) {
	const secret = "s3cret"
	token, _ := auth.MakeJWT(uuid.New(), secret, time.Hour)
	for name, body := range map[string]string{
		"not json":         `nope`,
		"missing password": `{"email":"a@b.c"}`,
		"missing email":    `{"password":"pw"}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &apiConfig{jwtSecret: secret}
			req := httptest.NewRequest(http.MethodPut, "/api/users", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			cfg.handlerUpdateUser(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandlerDeleteChirpRejectsBadRequests(t *testing.T) {
	const secret = "s3cret"
	userID := uuid.New()
	valid, _ := auth.MakeJWT(userID, secret, time.Hour)
	expired, _ := auth.MakeJWT(userID, secret, -time.Minute)

	tests := []struct {
		name     string
		header   string
		chirpID  string
		wantCode int
	}{
		{"no header", "", uuid.NewString(), http.StatusUnauthorized},
		{"not bearer", "Basic abc", uuid.NewString(), http.StatusUnauthorized},
		{"malformed token", "Bearer nope", uuid.NewString(), http.StatusUnauthorized},
		{"expired token", "Bearer " + expired, uuid.NewString(), http.StatusUnauthorized},
		{"invalid chirp ID", "Bearer " + valid, "nope", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &apiConfig{jwtSecret: secret}
			req := httptest.NewRequest(http.MethodDelete, "/api/chirps/"+tt.chirpID, nil)
			req.SetPathValue("chirpID", tt.chirpID)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			cfg.handlerDeleteChirp(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestHandlerPolkaWebhookIgnoresOtherEvents(t *testing.T) {
	// A nil db proves these events never reach the database.
	cfg := &apiConfig{polkaKey: "key"}
	for name, body := range map[string]string{
		"other event":         `{"event":"user.payment_failed","data":{"user_id":"3311741c-680c-4546-99f3-fc9efac2036c"}}`,
		"other event no data": `{"event":"user.created"}`,
		"empty event":         `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/polka/webhooks", strings.NewReader(body))
			req.Header.Set("Authorization", "ApiKey key")
			rec := httptest.NewRecorder()
			cfg.handlerPolkaWebhook(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", rec.Body.String())
			}
		})
	}
}

func TestHandlerPolkaWebhookRejectsBadRequests(t *testing.T) {
	cfg := &apiConfig{polkaKey: "key"}
	for name, body := range map[string]string{
		"not json":        `nope`,
		"invalid user_id": `{"event":"user.upgraded","data":{"user_id":"nope"}}`,
		"missing user_id": `{"event":"user.upgraded"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/polka/webhooks", strings.NewReader(body))
			req.Header.Set("Authorization", "ApiKey key")
			rec := httptest.NewRecorder()
			cfg.handlerPolkaWebhook(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandlerPolkaWebhookRequiresAPIKey(t *testing.T) {
	const body = `{"event":"user.upgraded","data":{"user_id":"3311741c-680c-4546-99f3-fc9efac2036c"}}`
	tests := []struct {
		name     string
		polkaKey string
		header   string
	}{
		{"no header", "key", ""},
		{"wrong key", "key", "ApiKey nope"},
		{"key as bearer", "key", "Bearer key"},
		{"no scheme", "key", "key"},
		{"empty key in header", "key", "ApiKey "},
		{"unset server key", "", "ApiKey "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A nil db proves unauthenticated requests never reach it.
			cfg := &apiConfig{polkaKey: tt.polkaKey}
			req := httptest.NewRequest(http.MethodPost, "/api/polka/webhooks", strings.NewReader(body))
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			cfg.handlerPolkaWebhook(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestUserFromDB(t *testing.T) {
	id := uuid.New()
	got := userFromDB(database.User{ID: id, Email: "a@b.c", HashedPassword: "secret", IsChirpyRed: true})
	if got.ID != id || got.Email != "a@b.c" || !got.IsChirpyRed {
		t.Errorf("userFromDB = %+v", got)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"is_chirpy_red":true`) || strings.Contains(string(b), "secret") {
		t.Errorf("json = %s", b)
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
