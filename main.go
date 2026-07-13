package main

import (
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

func (cfg *apiConfig) metricsShow(resW http.ResponseWriter, req *http.Request) {
	resW.Header().Set("Content-Type", "text/html; charset=utf-8")
	resW.WriteHeader(200)
	fmt.Fprintf(resW, `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load())
}

func (cfg *apiConfig) metricsReset(resW http.ResponseWriter, req *http.Request) {
	cfg.fileserverHits.Store(0)
}

func main() {
	mux := http.NewServeMux()

	var server http.Server
	server.Handler = mux
	server.Addr = ":8080"

	cfg := apiConfig{}
	apiCfg := &cfg

	fs := http.FileServer(http.Dir("."))

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fs)))
	mux.HandleFunc("POST /admin/reset", apiCfg.metricsReset)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsShow)

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		w.WriteHeader(200)

		w.Write([]byte("OK"))
	})

	server.ListenAndServe()
}
