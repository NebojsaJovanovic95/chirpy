package main

import (
	"log"
	"fmt"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg := &apiConfig{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"OK"}`))
	})
	mux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "text/plain")
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

	log.Println("Listening on http://localhost", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
