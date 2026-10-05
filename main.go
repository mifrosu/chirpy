package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/lib/pq"
	"github.com/scruffling/chirpy/internal/database"
)

type apiHandler struct{}

type apiConfig struct {
	// safely write and read an int across go routines
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// handlerMetrics reports the number of file server requests counted so far
// as an HTML page.
func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>
`, cfg.fileserverHits.Load())
}

// handlerReset zeroes the hit counter and deletes all users. It is only
// allowed when PLATFORM is "dev"; otherwise it responds with 403.
func (cfg *apiConfig) handlerReset(w http.ResponseWriter, req *http.Request) {
	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "Forbidden")
		return
	}
	if err := cfg.db.DeleteUsers(req.Context()); err != nil {
		slog.Error("delete users", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete users")
		return
	}
	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hits reset to 0\n"))
}

func (apiHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func handlerReadiness(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

const maxChirpLength = 140

func respondWithJSON(w http.ResponseWriter, code int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal response", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, map[string]string{"error": msg})
}

// handlerValidateChirp checks that a chirp body is at most 140 characters and
// responds with the body cleaned of profane words.
func handlerValidateChirp(w http.ResponseWriter, req *http.Request) {
	var params struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
		slog.Error("decode chirp", "err", err)
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if utf8.RuneCountInString(params.Body) > maxChirpLength {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"cleaned_body": cleanProfanity(params.Body)})
}

// User is the JSON representation of a user returned by the API.
type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

// handlerCreateUser creates a user from the email in the request body and
// responds with 201 and the new user.
func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, req *http.Request) {
	var params struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
		slog.Error("decode user", "err", err)
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}
	if params.Email == "" {
		respondWithError(w, http.StatusBadRequest, "Email is required")
		return
	}

	// We pass request context to the db so that the operation may cancel
	// if the query is interupted
	user, err := cfg.db.CreateUser(req.Context(), params.Email)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" { // unique_violation
			respondWithError(w, http.StatusConflict, "A user with that email already exists")
			return
		}
		slog.Error("create user", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user")
		return
	}

	respondWithJSON(w, http.StatusCreated, User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})
}

var profaneWords = map[string]bool{
	"kerfuffle": true,
	"sharbert":  true,
	"fornax":    true,
}

// cleanProfanity replaces each profane word (case-insensitive) with ****.
// Words are split on spaces, so a word with attached punctuation such as
// "Sharbert!" is treated as a different word and left alone.
func cleanProfanity(body string) string {
	words := strings.Split(body, " ")
	for i, word := range words {
		if profaneWords[strings.ToLower(word)] {
			words[i] = "****"
		}
	}
	return strings.Join(words, " ")
}

func main() {
	godotenv.Load()
	// TextHandler emits RFC 3339 (ISO 8601) timestamps; convert them to UTC.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	if err := db.Ping(); err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	dbQueries := database.New(db)

	const port = "8080"
	const staticPath = "./static"

	cfg := &apiConfig{db: dbQueries, platform: os.Getenv("PLATFORM")}

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler{})
	mux.HandleFunc("GET /api/healthz", handlerReadiness)
	mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)
	mux.HandleFunc("POST /api/users", cfg.handlerCreateUser)
	mux.HandleFunc("GET /admin/metrics", cfg.handlerMetrics)
	mux.HandleFunc("POST /admin/reset", cfg.handlerReset)
	// Serve only ./static under /app/; FileServer serves static/app/index.html
	// for "/app/" and 404s anything that doesn't exist. Mounting at "/app/"
	// lets the mux redirect "/app" itself, so the redirect isn't counted by
	// the metrics middleware (which counts each request to this handler).
	mux.Handle("/app/", cfg.middlewareMetricsInc(http.FileServer(http.Dir(staticPath))))

	server := &http.Server{
		Addr:           ":" + port,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20, // bitwise left shift, 2 power 20 (1 MB)
	}

	slog.Info("Serving", "port", port, "path", staticPath)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
