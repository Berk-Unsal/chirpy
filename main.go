package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Berk-Unsal/chirpy/internal/auth"
	"github.com/Berk-Unsal/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	IsChirpyRed  bool      `json:"is_chirpy_red"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

// apiConfig stores shared application state.
//
// atomic.Int32 allows the counter to be safely updated by multiple
// HTTP requests running concurrently.
type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
	secret_key     string
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
	if cfg.platform != "dev" {
		resW.Header().Set("Content-Type", "text/plain")
		resW.WriteHeader(http.StatusForbidden)
		resW.Write([]byte("You don't have access to this function."))
		return
	}
	cfg.fileserverHits.Store(0)
	ctx := req.Context()

	err := cfg.dbQueries.WipeUsers(ctx)
	if err != nil {
		resW.Header().Set("Content-Type", "text/plain")
		resW.WriteHeader(http.StatusInternalServerError)
		resW.Write([]byte("There was an issue deleting users."))
		return
	}

	resW.Header().Set("Content-Type", "text/plain; charset=utf-8")
	resW.WriteHeader(http.StatusOK)
	resW.Write([]byte("Users wiped successfully."))

}

func (cfg *apiConfig) createChirp(w http.ResponseWriter, r *http.Request) {
	// This struct represents the JSON sent by the client.
	type parameters struct {
		Body string `json:"body"`
	}

	// The decoder reads JSON directly from the HTTP request body.
	decoder := json.NewDecoder(r.Body)

	// params starts with its zero values and is filled by Decode.
	params := parameters{}

	err := decoder.Decode(&params)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("There was an issue decoding the parameters."))
		return
	}
	token_string, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusForbidden, "There an issue getting the bearer token")
		return
	}
	validatedUserId, err := auth.ValidateJWT(token_string, cfg.secret_key)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error validating the token")
		return
	}
	// The decoded Body field remains available for validation.
	chirpLen := len(params.Body)

	if chirpLen > 140 {
		respondWithError(
			w,
			http.StatusBadRequest,
			"Chirp is too long",
		)
		return
	}
	params.Body = returnCleanedBody(params.Body)

	ctx := r.Context()

	chirpParameters := database.CreateChirpParams{
		Body:   params.Body,
		UserID: validatedUserId,
	}

	chirp, err := cfg.dbQueries.CreateChirp(ctx, chirpParameters)

	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		msg := fmt.Sprintf("There wasn a issue getting the chirp data | user id: %v : %v", chirp.UserID, err)
		w.Write([]byte(msg))
		return
	}
	chirpsStructured := Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}

	respondWithJSON(w, http.StatusCreated, chirpsStructured)
}
func (cfg *apiConfig) userAPI(w http.ResponseWriter, r *http.Request) {
	type parameter struct {
		Email    string `json:"email"`
		Password string `password:"password"`
	}
	param := parameter{}

	decoder := json.NewDecoder(r.Body)

	err := decoder.Decode(&param)

	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("There was an issue decoding the parameters."))
		return
	}
	hashed_password, err := auth.HashPassword(param.Password)
	if err != nil {
		respondWithError(w, 500, "There was an issue hashing the password")
		return
	}
	userParams := database.CreateUserParams{
		Email:          param.Email,
		HashedPassword: hashed_password,
	}

	ctx := r.Context()

	user, err := cfg.dbQueries.CreateUser(ctx, userParams)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("There was an issue getting the user data."))
		return
	}
	usersStructured := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}

	respondWithJSON(w, http.StatusCreated, usersStructured)
}

func (cfg *apiConfig) getAllChirps(w http.ResponseWriter, r *http.Request) {
	var sorting string
	ctx := r.Context()
	s := r.URL.Query().Get("author_id")
	s2 := r.URL.Query().Get("sort")
	if s2 == "asc" {
		sorting = "asc"
	} else if s2 == "desc" {
		sorting = "desc"
	} else {
		respondWithError(w, http.StatusBadRequest, "Unexpected sorting query.")
		return
	}
	var chirps []database.Chirp
	if s != "" {
		uid, err := uuid.Parse(s)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "There was an issue parsing the user id ")
			return
		}
		chirps, err = cfg.dbQueries.GetChirpsFromUser(ctx, uid)
	} else {
		var err error
		chirps, err = cfg.dbQueries.GetAllChirps(ctx)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "There was an issue getting all the chirps.")
			return
		}
	}

	lenChirps := len(chirps)
	if lenChirps < 1 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Chirps are empty."))
		return
	}

	structuredChirps := []Chirp{}
	for _, chirp := range chirps {
		structuredChirp := Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		}
		structuredChirps = append(structuredChirps, structuredChirp)
	}

	if sorting == "asc" {
		sort.Slice(structuredChirps, func(i, j int) bool {
			return structuredChirps[i].CreatedAt.Before(structuredChirps[j].CreatedAt)
		})
	} else if sorting == "desc" {
		sort.Slice(structuredChirps, func(i, j int) bool {
			return structuredChirps[i].CreatedAt.After(structuredChirps[j].CreatedAt)
		})
	}
	respondWithJSON(w, 200, structuredChirps)
}

func (cfg *apiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, 400, "There was an issue parsing the endpoint.")
	}
	ctx := r.Context()

	chirps, err := cfg.dbQueries.GetAllChirps(ctx)
	if err != nil {
		respondWithError(w, 500, "There was an issue getting the chirps")
	}
	for _, chirp := range chirps {
		if chirp.ID == userID {
			structuredChirp := Chirp{
				ID:        chirp.ID,
				CreatedAt: chirp.CreatedAt,
				UpdatedAt: chirp.UpdatedAt,
				Body:      chirp.Body,
				UserID:    chirp.UserID,
			}
			respondWithJSON(w, 200, structuredChirp)
			return
		}
	}

	respondWithError(w, 404, "Chirp not found.")
}

func (cfg *apiConfig) checkLogin(w http.ResponseWriter, r *http.Request) {
	type parameter struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	param := parameter{}

	decoder := json.NewDecoder(r.Body)
	ctx := r.Context()
	err := decoder.Decode(&param)

	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("There was an issue decoding the parameters."))
		return
	}
	expectedHash, err := cfg.dbQueries.GetUserPassword(ctx, param.Email)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue getting the hashed password.")
		return
	}

	bool, err := auth.CheckPasswordHash(param.Password, expectedHash)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue checking the hashed password.")
		return
	}
	if !bool {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}
	user, err := cfg.dbQueries.GetUser(ctx, param.Email)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue getting the user data")
		return
	}
	token, err := auth.MakeJWT(user.ID, cfg.secret_key, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue creating the jwt token")
		return
	}

	refresh_token := auth.MakeRefreshToken()
	refresh_parameters := database.CreateRefreshTokenParams{
		Token:     refresh_token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour * 24 * 60),
	}
	rf, err := cfg.dbQueries.CreateRefreshToken(ctx, refresh_parameters)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "there was an issue creating the refresh token.")
		return
	}

	userStructured := User{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		Token:        token,
		RefreshToken: rf.Token,
		IsChirpyRed:  user.IsChirpyRed.Bool,
	}

	respondWithJSON(w, http.StatusOK, userStructured)
}

func (cfg *apiConfig) refreshToken(w http.ResponseWriter, r *http.Request) {
	bearer, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusForbidden, "There was an issue getting the token bearer.")
		return
	}
	ctx := r.Context()
	userid, err := cfg.dbQueries.GetUserFromRefreshToken(ctx, bearer)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error getting the user from the refresh token.")
		return
	}
	newJWT, err := auth.MakeJWT(userid, cfg.secret_key, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an error creating the new JWT")
		return
	}

	type parameter struct {
		Token string `json:"token"`
	}
	newParameter := parameter{Token: newJWT}
	respondWithJSON(w, http.StatusOK, newParameter)
}

func (cfg *apiConfig) revokeRefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bearer, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusForbidden, "There was an error authenticating the bearer token.")
		return
	}
	err = cfg.dbQueries.RevokeRefreshToken(ctx, bearer)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an error revoking the token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token_string, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error getting the token bearer")
		return
	}
	validatedUserId, err := auth.ValidateJWT(token_string, cfg.secret_key)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error getting the user id.")
		return
	}

	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	var param parameters
	decoder := json.NewDecoder(r.Body)
	err = decoder.Decode(&param)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue decoding the parameters")
		return
	}

	hashed_password, err := auth.HashPassword(param.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an error hashing the password.")
		return
	}
	structuredParameters := database.UpdateUserParams{
		HashedPassword: hashed_password,
		Email:          param.Email,
		ID:             validatedUserId,
	}

	err = cfg.dbQueries.UpdateUser(ctx, structuredParameters)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an error updating the user.")
		return
	}
	user, err := cfg.dbQueries.GetUser(ctx, param.Email)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an error fetching the user data")
		return
	}
	userStructured := User{
		ID:        validatedUserId,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}
	respondWithJSON(w, http.StatusOK, userStructured)
}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}
	token_string, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error getting the token bearer")
		return
	}
	validatedUserId, err := auth.ValidateJWT(token_string, cfg.secret_key)
	if err != nil {
		respondWithError(w, http.StatusForbidden, "There was an error getting the user id.")
		return
	}
	chirp, err := cfg.dbQueries.GetChirp(ctx, chirpID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}
	if chirp.UserID != validatedUserId {
		respondWithError(w, http.StatusForbidden, "User doesn't have access to the chirp")
		return
	}
	err = cfg.dbQueries.DeleteChirp(ctx, chirpID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue deleting the chirp.")
		return
	}
	w.WriteHeader(http.StatusNoContent)

}

func (cfg *apiConfig) userUpgrade(w http.ResponseWriter, r *http.Request) {
	type data struct {
		UserId uuid.UUID `json:"user_id"`
	}
	type parameters struct {
		Event string `json:"event"`
		Data  data   `json:"data"`
	}
	param := parameters{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&param)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue decoding the request.")
		return
	}

	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error getting the apikey")
		return
	}
	envApiKey := os.Getenv("POLKA_KEY")
	if envApiKey != apiKey {
		respondWithError(w, http.StatusUnauthorized, "Api key is invalid")
		return
	}
	if param.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx := r.Context()
	_, err = cfg.dbQueries.UpgradeUserToRed(ctx, param.Data.UserId)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "There was an error ugprading the user: user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		flag = slices.Contains(badWords, strings.ToLower(word))
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
	platform := os.Getenv("PLATFORM")
	secret_key := os.Getenv("SECRET_KEY")

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
	cfg.platform = platform
	cfg.secret_key = secret_key

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
	mux.HandleFunc("POST /api/login", apiCfg.checkLogin)
	mux.HandleFunc("POST /api/users", apiCfg.userAPI)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.getAllChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirp)
	mux.HandleFunc("POST /api/refresh", apiCfg.refreshToken)
	mux.HandleFunc("POST /api/revoke", apiCfg.revokeRefreshToken)
	mux.HandleFunc("PUT /api/users", apiCfg.updateUser)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.deleteChirp)
	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.userUpgrade)
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
