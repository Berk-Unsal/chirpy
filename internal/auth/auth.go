package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	return hash, err
}

func CheckPasswordHash(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	return match, err
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(expiresIn)),
		Subject:   userID.String(),
	})
	return token.SignedString([]byte(tokenSecret))
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claims := jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(tokenSecret), nil
	})
	if err != nil {
		var empty uuid.UUID
		return empty, err
	}
	s, err := token.Claims.GetSubject()
	if err != nil {
		var empty uuid.UUID
		return empty, err
	}
	id, err := uuid.Parse(s)
	if err != nil {
		var empty uuid.UUID
		return empty, err
	}
	return id, nil
}

func GetBearerToken(header http.Header) (string, error) {
	bearer := header.Get("Authorization")
	if bearer == "" {
		return "", fmt.Errorf("Unexpected bearer : bearer is empty")
	}
	clean := strings.Split(bearer, " ")
	if len(clean) < 2 {
		return "", fmt.Errorf("Unexpected format of the bearer")
	}
	TOKEN_STRING := clean[1]

	return TOKEN_STRING, nil
}

func MakeRefreshToken() string {
	b := make([]byte, 256)
	_, err := rand.Read(b)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
