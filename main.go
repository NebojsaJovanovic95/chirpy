package main

import (
	"os"
	"context"
	"time"
	"database/sql"
	"log"
	"fmt"
	"strings"
	"net/http"
	"encoding/json"
	"sync/atomic"
	"github.com/joho/godotenv"
	"github.com/NebojsaJovanovic95/chirpy/internal/auth"
	"github.com/NebojsaJovanovic95/chirpy/internal/database"
	_ "github.com/lib/pq"
	"github.com/google/uuid"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

type validateChirpRequest struct {
	Body string `json:"body"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    uuid.UUID `json:"user_id"`
	Body      string    `json:"body"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, map[string]string{
		"error": msg,
	})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	data, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(data)
}

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

	// create a user
	_, err = dbQueries.CreateUser(context.Background(), "test@example.com")
	if err != nil {
		log.Fatal(err)
	}
	
	cfg := &apiConfig{
		db:       dbQueries,
		platform: os.Getenv("PLATFORM"),
	}

	mux := http.NewServeMux()
	
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		defer r.Body.Close()

		var req struct {
			Email string `json:"email"`
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
			Email:				req.Email,
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
	})
	
	mux.HandleFunc("/api/chirps/", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.HandleFunc("/api/chirps", func(w http.ResponseWriter, r *http.Request) {
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

			// validate chirp length
			if len(req.Body) > 140 {
				respondWithError(w, http.StatusBadRequest, "chirp is too long")
				return
			}

			// profanity filtering
			words := strings.Split(req.Body, " ")
			profanity := map[string]bool{
				"kerfuffle": true,
				"sharbert":  true,
				"fornax":    true,
			}
			for i, word := range words {
				if profanity[strings.ToLower(word)] {
					words[i] = "****"
				}
			}
			cleaned := strings.Join(words, " ")

			// convert string userID to uuid.UUID
			userUUID, err := uuid.Parse(req.UserID)
			if err != nil {
				respondWithError(w, http.StatusBadRequest, "invalid user_id")
				return
			}

			// create chirp
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
	})

	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"OK"}`))
	})
	mux.HandleFunc("/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `
<html>
	<body>
		<h1>
			Welcome, Chirpy Admin
		</h1>
		<p>
			Chirpy has been visited %d times!
		</p>
	</body>
</html>
`, cfg.fileserverHits.Load())
	})
	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		if cfg.platform != "dev" {
			respondWithError(w, http.StatusForbidden, "forbidden")
			return
		}
		if err := cfg.db.DeleteAllUsers(r.Context()); err != nil {
			respondWithError(w, http.StatusInternalServerError, "failed to delete users")
		}
		cfg.fileserverHits.Store(0)
		w.WriteHeader(http.StatusOK)
	})
	fileServer := cfg.middlewareMetricsInc(
		http.FileServer(http.Dir(".")),
	)
	mux.Handle("/app/", http.StripPrefix("/app", fileServer))
	server := &http.Server{
		Addr: ":8080",
		Handler: mux,
	}

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		var req struct {
			Email string `json:"email"`
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
	})

	log.Println("Listening on http://localhost", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
