package main

import (
	_ "context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/NebojsaJovanovic95/chirpy/internal/auth"
	"github.com/NebojsaJovanovic95/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits	atomic.Int32
	db							*database.Queries
	platform				string
	jwtSecret				string
	polkaKey				string
}

type loginRequest struct {
	Email							string	`json:"email"`
	Password					string	`json:"password"`
	ExpiresInSeconds	*int		`json:"expires_in_seconds"`
}

type Chirp struct {
	ID				uuid.UUID	`json:"id"`
	CreatedAt	time.Time	`json:"created_at"`
	UpdatedAt	time.Time	`json:"updated_at"`
	UserID		uuid.UUID	`json:"user_id"`
	Body			string		`json:"body"`
}

// --- Utilities ---

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, map[string]string{"error": msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if data, err := json.Marshal(payload); err == nil {
		w.Write(data)
	}
}

// --- Handlers ---

func (cfg *apiConfig) handlePolkaWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil || apiKey != cfg.polkaKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	
	defer r.Body.Close()

	var payload struct {
		Event string `json:"event"`
		Data struct {
			UserID uuid.UUID `json:"user_id"`
		} `json:"data"`
	}

	if err :=  json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	if payload.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := cfg.db.UpgradeUserToChirpyRed(r.Context(), payload.Data.UserID); err != nil {
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		cfg.handleUpdateUser(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user, err := cfg.db.CreateUserWithPassword(r.Context(), database.CreateUserWithPasswordParams{
		Email:          req.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	w.WriteHeader(http.StatusCreated)
	respondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         user.ID,
		"email":      user.Email,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
		"is_chirpy_red": user.IsChirpyRed,
	})
}

func (cfg *apiConfig) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	userID, err := auth.ValidateJWT(tokenString, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	defer r.Body.Close()
	var req struct{
		Email			string `json:"email"`
		Password	string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user, err := cfg.db.UpdateUser(r.Context(), database.UpdateUserParams{
		ID:						userID,
		Email:				req.Email,
		HashedPassword:	hashedPassword,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":					user.ID,
		"email":			user.Email,
		"created_at":	user.CreatedAt,
		"updated_at":	user.UpdatedAt,
		"is_chirpy_red": user.IsChirpyRed,
	})
}

func (cfg *apiConfig) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := cfg.db.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	match, err := auth.CheckPasswordHash(req.Password, user.HashedPassword)
	if err != nil || !match {
		respondWithError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	expires := time.Hour
	if req.ExpiresInSeconds != nil {
		requested := time.Duration(*req.ExpiresInSeconds) * time.Second
		if requested < expires {
			expires = requested
		}
	}

	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, expires)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not create token")
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to create refresh token")
		return
	}
	err = cfg.db.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token:		refreshToken,
		UserID:		uuid.NullUUID{UUID: user.ID, Valid: true},
		ExpiresAt:	time.Now().Add(60 * 24 * time.Hour),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to store refresh token")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":							user.ID,
		"email":					user.Email,
		"created_at":			user.CreatedAt,
		"updated_at":			user.UpdatedAt,
		"is_chirpy_red": user.IsChirpyRed,
		"token":					token,
		"refresh_token":	refreshToken,
	})
}

func (cfg *apiConfig) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}
	user, err := cfg.db.GetUserFromRefreshToken(r.Context(), refreshToken)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	tokenRow, err := cfg.db.GetRefreshToken(r.Context(), refreshToken)
	if err != nil || !tokenRow.RevokedAt.Valid && tokenRow.ExpiresAt.Before(time.Now()) {
		respondWithError(w, http.StatusUnauthorized, "refresh token expired or revoked")
		return
	}

	newToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not create access token")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"token": newToken})
}

func (cfg *apiConfig) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	err = cfg.db.RevokeRefreshToken(r.Context(), database.RevokeRefreshTokenParams{
		Token:     refreshToken,
		RevokedAt: sql.NullTime{
			Time:		time.Now(),
			Valid:	true,
		},
		UpdatedAt: time.Now(),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent) // 204
}

func (cfg *apiConfig) handleChirps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		defer r.Body.Close()

		tokenString, err := auth.GetBearerToken(r.Header)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "missing or invalid auth token")
			return
		}
		userID, err := auth.ValidateJWT(tokenString, cfg.jwtSecret)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		var req struct {
			Body   string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if len(req.Body) > 140 {
			respondWithError(w, http.StatusBadRequest, "chirp is too long")
			return
		}

		words := strings.Split(req.Body, " ")
		profanity := map[string]bool{"kerfuffle": true, "sharbert": true, "fornax": true}
		for i, word := range words {
			if profanity[strings.ToLower(word)] {
				words[i] = "****"
			}
		}
		cleaned := strings.Join(words, " ")

		chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
			Body:   cleaned,
			UserID: userID,
		})
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "failed to create chirp")
			return
		}

		respondWithJSON(w, http.StatusCreated, Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		})
	case http.MethodGet:
		authorIDStr := r.URL.Query().Get("author_id")
		var chirps []database.Chirp
		var err error
		
		if authorIDStr == "" {
			chirps, err = cfg.db.GetChirps(r.Context())
		} else {
			authorID, parseErr := uuid.Parse(authorIDStr)
			if parseErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			chirps, err = cfg.db.GetChirpsByAuthor(r.Context(), authorID)
		}

		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "failed to fetch chirps")
			return
		}
		result := make([]Chirp, 0, len(chirps))
		for _, c := range chirps {
			result = append(result, Chirp{
				ID:        c.ID,
				CreatedAt: c.CreatedAt,
				UpdatedAt: c.UpdatedAt,
				Body:      c.Body,
				UserID:    c.UserID,
			})
		}
		respondWithJSON(w, http.StatusOK, result)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (cfg *apiConfig) handleChirpByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/chirps/")
	chirpID, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid chirp id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		chirp, err := cfg.db.GetChirp(r.Context(), chirpID)
		if err != nil {
			if err == sql.ErrNoRows {
				respondWithError(w, http.StatusNotFound, "chirp not found")
				return
			}
			respondWithError(w, http.StatusInternalServerError, "failed to fetch chirp")
			return
		}

		respondWithJSON(w, http.StatusOK, Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		})

	case http.MethodDelete:
		tokenString, err := auth.GetBearerToken(r.Header)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		userID, err := auth.ValidateJWT(tokenString, cfg.jwtSecret)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		chirp, err := cfg.db.GetChirp(r.Context(), chirpID)
		if err != nil {
			if err == sql.ErrNoRows {
				respondWithError(w, http.StatusNotFound, "chirp not found")
				return
			}
			respondWithError(w, http.StatusInternalServerError, "failed to fetch chirp")
			return
		}
		
		if chirp.UserID != userID {
			respondWithError(w, http.StatusForbidden, "forbidden")
			return
		}

		if err := cfg.db.DeleteChirp(r.Context(), chirpID); err != nil {
			respondWithError(w, http.StatusInternalServerError, "failed to delete chirp")
			return
		}

		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

// --- Main ---

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET not set")
	}
	polkaKey := os.Getenv("POLKA_KEY")
	if polkaKey == "" {
		log.Fatal("POLKA_KEY not set")
	}
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	dbQueries := database.New(db)
	cfg := &apiConfig{
		db:					dbQueries,
		platform:		os.Getenv("PLATFORM"),
		jwtSecret:	jwtSecret,
		polkaKey:		polkaKey,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/polka/webhooks", cfg.handlePolkaWebhook)
	mux.HandleFunc("/api/users", cfg.handleUsers)
	mux.HandleFunc("/api/login", cfg.handleLogin)
	mux.HandleFunc("/api/chirps", cfg.handleChirps)
	mux.HandleFunc("/api/chirps/", cfg.handleChirpByID)
	mux.HandleFunc("/api/refresh", cfg.handleRefresh)
	mux.HandleFunc("/api/revoke", cfg.handleRevoke)


	// Health & admin
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"OK"}`))
	})

	mux.HandleFunc("/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<h1>Chirpy visited %d times</h1>", cfg.fileserverHits.Load())
	})

	mux.HandleFunc("/admin/reset", func(w http.ResponseWriter, r *http.Request) {
		if cfg.platform != "dev" {
			respondWithError(w, http.StatusForbidden, "forbidden")
			return
		}
		if err := cfg.db.DeleteAllUsers(r.Context()); err != nil {
			respondWithError(w, http.StatusInternalServerError, "failed to delete users")
			return
		}
		cfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})

	fileServer := cfg.middlewareMetricsInc(http.FileServer(http.Dir(".")))
	mux.Handle("/app/", http.StripPrefix("/app", fileServer))

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Listening on http://localhost:8080")
	log.Fatal(server.ListenAndServe())
}
