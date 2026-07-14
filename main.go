package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/Berk-Unsal/chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// apiConfig stores shared application state.
//
// atomic.Int32 allows the counter to be safely updated by multiple
// HTTP requests running concurrently.
type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
}

// middlewareMetricsInc wraps another HTTP handler.
//
// Every request that passes through this middleware increments the
// file server hit counter before continuing to the original handler.
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)

		// Pass the request to the wrapped handler.
		next.ServeHTTP(w, r)
	})
}

// metricsShow returns an HTML page displaying the current file server hit count.
func (cfg *apiConfig) metricsShow(resW http.ResponseWriter, req *http.Request) {
	resW.Header().Set("Content-Type", "text/html; charset=utf-8")
	resW.WriteHeader(http.StatusOK)

	fmt.Fprintf(resW, `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load())
}

// metricsReset resets the file server hit counter to zero.
func (cfg *apiConfig) metricsReset(resW http.ResponseWriter, req *http.Request) {
	cfg.fileserverHits.Store(0)
}

// chirpCheckLen validates the JSON body of a chirp request.
//
// The request is expected to contain JSON in this form:
//
//	{
//	    "body": "chirp text"
//	}
func (cfg *apiConfig) chirpCheckLen(resW http.ResponseWriter, req *http.Request) {
	// This struct represents the JSON sent by the client.
	type parameters struct {
		Body string `json:"body"`
	}

	// The decoder reads JSON directly from the HTTP request body.
	decoder := json.NewDecoder(req.Body)

	// params starts with its zero values and is filled by Decode.
	params := parameters{}

	err := decoder.Decode(&params)
	if err != nil {
		resW.Header().Set("Content-Type", "text/plain")
		resW.WriteHeader(http.StatusBadRequest)
		resW.Write([]byte("There was an issue decoding the parameters."))
		return
	}

	// The decoded Body field remains available for validation.
	chirpLen := len(params.Body)

	if chirpLen > 140 {
		respondWithError(
			resW,
			http.StatusBadRequest,
			"Chirp is too long",
		)
		return
	}

	// This struct represents the JSON returned for a successful validation.
	//
	// It is separate from parameters because the request and response
	// contain different fields.

	type cleanedBody struct {
		Body string `json:"cleaned_body"`
	}

	response := cleanedBody{
		Body: returnCleanedBody(params.Body),
	}

	respondWithJSON(resW, http.StatusOK, response)
}

// respondWithError creates a structured error response.
//
// It does not marshal the struct itself. It delegates that responsibility
// to respondWithJSON so that JSON handling remains in one place.
func respondWithError(w http.ResponseWriter, code int, msg string) {
	// This struct determines the shape of the JSON error response.
	type returnErr struct {
		Error string `json:"error"`
	}

	response := returnErr{
		Error: msg,
	}

	respondWithJSON(w, code, response)
}

// respondWithJSON converts any supported Go value into JSON and writes it
// as an HTTP response.
//
// payload is interface{} because this helper may receive different concrete
// response types, such as a success struct or an error struct.
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	// Marshal converts the concrete Go value stored inside payload
	// into a JSON byte slice.
	data, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("There was an issue marshalling the data."))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func returnCleanedBody(sentence string) string {
	badWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Fields(sentence)
	replace := "****"
	var cleanedWord string
	flag := false
	for idx, word := range words {
		for _, badword := range badWords {
			if strings.ToLower(word) == badword {
				flag = true
				break
			}
		}
		if !flag {
			if idx == len(words)-1 {
				cleanedWord += word
			} else {
				cleanedWord += word + " "
			}
		} else {
			if idx == len(words)-1 {
				cleanedWord += replace
			} else {
				cleanedWord += replace + " "
			}
		}
		flag = false
	}
	return cleanedWord
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("There was a issue connecting to the database :%v\n", err)
		return
	}

	// ServeMux routes incoming requests to their matching handlers.
	mux := http.NewServeMux()

	// Configure the HTTP server to use the mux and listen on port 8080.
	var server http.Server
	server.Handler = mux
	server.Addr = ":8080"

	// Create the shared application configuration.
	cfg := apiConfig{}
	apiCfg := &cfg

	dbQueries := database.New(db)
	cfg.dbQueries = dbQueries

	// Serve files from the current working directory.
	fs := http.FileServer(http.Dir("."))

	// Strip "/app" before the file server resolves the requested file.
	//
	// The metrics middleware wraps only the file server, so requests to
	// other endpoints do not increment fileserverHits.
	mux.Handle(
		"/app/",
		apiCfg.middlewareMetricsInc(
			http.StripPrefix("/app", fs),
		),
	)

	// Register the application routes.
	mux.HandleFunc("POST /admin/reset", apiCfg.metricsReset)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsShow)
	mux.HandleFunc("POST /api/validate_chirp", apiCfg.chirpCheckLen)

	// The health endpoint provides a simple response showing that
	// the server is running.
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// ListenAndServe blocks while the server is running.
	server.ListenAndServe()
}
