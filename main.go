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
	"github.com/scruffling/chirpy/internal/auth"
	"github.com/scruffling/chirpy/internal/database"
)

type apiHandler struct{}

type apiConfig struct {
	// safely write and read an int across go routines
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
	jwtSecret      string
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

// Chirp is the JSON representation of a chirp returned by the API.
type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

var errChirpTooLong = errors.New("Chirp is too long")

// validateChirp checks that a chirp body is at most 140 characters and
// returns it cleaned of profane words.
func validateChirp(body string) (string, error) {
	if utf8.RuneCountInString(body) > maxChirpLength {
		return "", errChirpTooLong
	}
	return cleanProfanity(body), nil
}

func chirpFromDB(c database.Chirp) Chirp {
	return Chirp{
		ID:        c.ID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Body:      c.Body,
		UserID:    c.UserID,
	}
}

// handlerGetChirps responds with all chirps as a JSON array, oldest first.
func (cfg *apiConfig) handlerGetChirps(w http.ResponseWriter, req *http.Request) {
	dbChirps, err := cfg.db.GetChirps(req.Context())
	if err != nil {
		slog.Error("get chirps", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't get chirps")
		return
	}

	// Non-nil so an empty result encodes as [] rather than null.
	chirps := make([]Chirp, 0, len(dbChirps))
	for _, c := range dbChirps {
		chirps = append(chirps, chirpFromDB(c))
	}
	respondWithJSON(w, http.StatusOK, chirps)
}

// handlerGetChirp responds with the chirp whose ID is in the request path.
func (cfg *apiConfig) handlerGetChirp(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
		return
	}

	chirp, err := cfg.db.GetChirp(req.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}
	if err != nil {
		slog.Error("get chirp", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't get chirp")
		return
	}
	respondWithJSON(w, http.StatusOK, chirpFromDB(chirp))
}

// handlerDeleteChirp deletes a chirp by ID and responds with 204. The request
// needs a valid access token (401 otherwise), the chirp must exist (404) and
// the token's user must be its author (403).
func (cfg *apiConfig) handlerDeleteChirp(w http.ResponseWriter, req *http.Request) {
	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	id, err := uuid.Parse(req.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
		return
	}

	chirp, err := cfg.db.GetChirp(req.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}
	if err != nil {
		slog.Error("get chirp", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete chirp")
		return
	}
	if chirp.UserID != userID {
		respondWithError(w, http.StatusForbidden, "Cannot delete another user's chirp")
		return
	}

	if err := cfg.db.DeleteChirp(req.Context(), id); err != nil {
		slog.Error("delete chirp", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete chirp")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handlerCreateChirp validates a chirp, saves it for the authenticated user and
// responds with 201 and the new chirp. The request must carry a valid JWT as a
// Bearer token (401 otherwise). The body's user_id is optional, but if present
// it must be the token's user (403 otherwise), so users can't post as others.
func (cfg *apiConfig) handlerCreateChirp(w http.ResponseWriter, req *http.Request) {
	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var params struct {
		Body   string `json:"body"`
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
		slog.Error("decode chirp", "err", err)
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if params.UserID != "" {
		bodyUserID, err := uuid.Parse(params.UserID)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid user_id")
			return
		}
		if bodyUserID != userID {
			respondWithError(w, http.StatusForbidden, "Cannot post a chirp as another user")
			return
		}
	}

	cleaned, err := validateChirp(params.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	chirp, err := cfg.db.CreateChirp(req.Context(), database.CreateChirpParams{
		Body:   cleaned,
		UserID: userID,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23503" { // foreign_key_violation
			respondWithError(w, http.StatusBadRequest, "User does not exist")
			return
		}
		slog.Error("create chirp", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't create chirp")
		return
	}

	respondWithJSON(w, http.StatusCreated, chirpFromDB(chirp))
}

// User is the JSON representation of a user returned by the API.
type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

// handlerCreateUser creates a user from the email and password in the request
// body, storing only an argon2id hash of the password, and responds with 201
// and the new user.
func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, req *http.Request) {
	var params struct {
		Email    string `json:"email"`
		Password string `json:"password"`
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
	if params.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Password is required")
		return
	}

	hashed, err := auth.HashPassword(params.Password)
	if err != nil {
		slog.Error("hash password", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user")
		return
	}

	// We pass request context to the db so that the operation may cancel
	// if the query is interupted
	user, err := cfg.db.CreateUser(req.Context(), database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashed,
	})
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

const accessTokenLifetime = time.Hour

// handlerUpdateUser updates the authenticated user's email and password and
// responds with 200 and the updated user. The user comes from the access token
// in the Authorization header (401 if it is missing or invalid), so users can
// only change their own details.
func (cfg *apiConfig) handlerUpdateUser(w http.ResponseWriter, req *http.Request) {
	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var params struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
		slog.Error("decode user update", "err", err)
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}
	if params.Email == "" || params.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	hashed, err := auth.HashPassword(params.Password)
	if err != nil {
		slog.Error("hash password", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't update user")
		return
	}

	user, err := cfg.db.UpdateUser(req.Context(), database.UpdateUserParams{
		ID:             userID,
		Email:          params.Email,
		HashedPassword: hashed,
	})
	if errors.Is(err, sql.ErrNoRows) { // valid token for a user that no longer exists
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" { // unique_violation
			respondWithError(w, http.StatusConflict, "A user with that email already exists")
			return
		}
		slog.Error("update user", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't update user")
		return
	}

	respondWithJSON(w, http.StatusOK, User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})
}

// handlerLogin checks the email and password in the request body and responds
// with 200, the user, a one-hour access token (JWT) and a 60-day refresh token
// stored in the database. Unknown emails and wrong passwords get the same 401
// so the response doesn't reveal which emails are registered.
func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, req *http.Request) {
	var params struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
		slog.Error("decode login", "err", err)
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}
	if params.Email == "" || params.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	const badCredentials = "Incorrect email or password"
	user, err := cfg.db.GetUserByEmail(req.Context(), params.Email)
	if errors.Is(err, sql.ErrNoRows) {
		respondWithError(w, http.StatusUnauthorized, badCredentials)
		return
	}
	if err != nil {
		slog.Error("get user", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't log in")
		return
	}

	// An error here (e.g. the 'unset' placeholder hash) is treated as a
	// failed login rather than a server error.
	match, err := auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if err != nil || !match {
		respondWithError(w, http.StatusUnauthorized, badCredentials)
		return
	}

	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, accessTokenLifetime)
	if err != nil {
		slog.Error("make jwt", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't log in")
		return
	}

	// revoked_at is left NULL on creation.
	refreshToken, err := cfg.db.CreateRefreshToken(req.Context(), database.CreateRefreshTokenParams{
		Token:  auth.MakeRefreshToken(),
		UserID: user.ID,
	})
	if err != nil {
		slog.Error("create refresh token", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't log in")
		return
	}

	respondWithJSON(w, http.StatusOK, struct {
		User
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}{
		User: User{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
		},
		Token:        token,
		RefreshToken: refreshToken.Token,
	})
}

// handlerRefresh takes a refresh token from the Authorization: Bearer header
// (no request body) and responds with 200 and a new one-hour access token for
// its user. A missing, unknown, expired or revoked token gets a 401.
func (cfg *apiConfig) handlerRefresh(w http.ResponseWriter, req *http.Request) {
	refreshToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// GetUserFromRefreshToken only matches tokens that are neither expired
	// nor revoked.
	user, err := cfg.db.GetUserFromRefreshToken(req.Context(), refreshToken)
	if errors.Is(err, sql.ErrNoRows) {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if err != nil {
		slog.Error("get user from refresh token", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't refresh token")
		return
	}

	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, accessTokenLifetime)
	if err != nil {
		slog.Error("make jwt", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't refresh token")
		return
	}

	respondWithJSON(w, http.StatusOK, struct {
		Token string `json:"token"`
	}{Token: token})
}

// handlerRevoke takes a refresh token from the Authorization: Bearer header
// (no request body), revokes it and responds with 204. Revoking an unknown or
// already-revoked token is not an error, so the call is idempotent; a missing
// header gets a 401.
func (cfg *apiConfig) handlerRevoke(w http.ResponseWriter, req *http.Request) {
	refreshToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := cfg.db.RevokeRefreshToken(req.Context(), refreshToken); err != nil {
		slog.Error("revoke refresh token", "err", err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("JWT_SECRET must be set")
		os.Exit(1)
	}

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

	cfg := &apiConfig{db: dbQueries, platform: os.Getenv("PLATFORM"), jwtSecret: jwtSecret}

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler{})
	mux.HandleFunc("GET /api/healthz", handlerReadiness)
	mux.HandleFunc("POST /api/chirps", cfg.handlerCreateChirp)
	mux.HandleFunc("GET /api/chirps", cfg.handlerGetChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", cfg.handlerGetChirp)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", cfg.handlerDeleteChirp)
	mux.HandleFunc("POST /api/users", cfg.handlerCreateUser)
	mux.HandleFunc("PUT /api/users", cfg.handlerUpdateUser)
	mux.HandleFunc("POST /api/login", cfg.handlerLogin)
	mux.HandleFunc("POST /api/refresh", cfg.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", cfg.handlerRevoke)
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
