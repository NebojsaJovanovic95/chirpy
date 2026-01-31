package main

import (
	"os"
	"context"
	"database/sql"
	"log"
	"fmt"
	"strings"
	"net/http"
	"encoding/json"
	"sync/atomic"
	"github.com/joho/godotenv"
	"github.com/NebojsaJovanovic95/chirpy/internal/database"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

type validateChirpRequest struct {
	Body string `json:"body"`
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
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		user, err := cfg.db.CreateUser(r.Context(), req.Email)
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
		cfg.fileserverHits.Store(0)
	})
	fileServer := cfg.middlewareMetricsInc(
		http.FileServer(http.Dir(".")),
	)
	mux.Handle("/app/", http.StripPrefix("/app", fileServer))
	server := &http.Server{
		Addr: ":8080",
		Handler: mux,
	}

	mux.HandleFunc("/api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		defer r.Body.Close()

		var req validateChirpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		if len(req.Body) > 140 {
			respondWithError(w, http.StatusBadRequest, "Chirp is too long")
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
			lowered := strings.ToLower(word)
			if profanity[lowered] {
				words[i] = "****"
			}
		}

		cleaned := strings.Join(words, " ")

		respondWithJSON(w, http.StatusOK, map[string]string{
			"cleaned_body": cleaned,
		})
	})
	
	log.Println("Listening on http://localhost", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
