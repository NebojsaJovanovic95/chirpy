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
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    uuid.UUID `json:"user_id"`
	Body      string    `json:"body"`
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

func (cfg *apiConfig) handleUsers(w http.ResponseWriter, r *http.Request) {
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
	})
}

func (cfg *apiConfig) handleLogin(w http.ResponseWriter, r *http.Request) {
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

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":         user.ID,
		"email":      user.Email,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
	})
}

func (cfg *apiConfig) handleChirps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		defer r.Body.Close()
		var req struct {
			Body   string `json:"body"`
			UserID string `json:"user_id"`
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

		userUUID, err := uuid.Parse(req.UserID)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid user_id")
			return
		}

		chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
			Body:   cleaned,
			UserID: userUUID,
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
		chirps, err := cfg.db.GetChirps(r.Context())
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
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/chirps/")
	chirpID, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid chirp id")
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

	respondWithJSON(w, http.StatusOK, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

// --- Main ---

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	dbQueries := database.New(db)
	cfg := &apiConfig{
		db:       dbQueries,
		platform: os.Getenv("PLATFORM"),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/users", cfg.handleUsers)
	mux.HandleFunc("/api/login", cfg.handleLogin)
	mux.HandleFunc("/api/chirps", cfg.handleChirps)
	mux.HandleFunc("/api/chirps/", cfg.handleChirpByID)

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
