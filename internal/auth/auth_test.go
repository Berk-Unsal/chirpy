package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMakeJWT(t *testing.T) {
	expectedId := uuid.New()
	tokenSecret := "fadfa"
	expiresIn := 5 * time.Minute
	token, err := MakeJWT(expectedId, tokenSecret, expiresIn)
	if err != nil {
		t.Errorf("there was a problem fetching the token: %v", err)
		return
	}
	gotId, err := ValidateJWT(token, tokenSecret)
	if err != nil {
		t.Errorf("there was a problem validating the jwt:%v ", err)
		return
	}
	if expectedId != gotId {
		t.Errorf("id's do not match, got :%v, expected:%v", gotId, expectedId)
		return
	}
}

func TestValidateJWT_Expired(t *testing.T) {
	expiresIn := time.Minute * -5
	expectedId := uuid.New()
	tokenSecret := "fadfa"
	token, err := MakeJWT(expectedId, tokenSecret, expiresIn)
	if err != nil {
		t.Errorf("there was a problem making the jwt:%v", err)
		return
	}
	_, err = ValidateJWT(token, tokenSecret)
	if err == nil {
		t.Errorf("there was a problem with the the validate jwt function, it shouldn't have allow expired jwt token")
	}
}

func TestValidateJWT_WrongSecret(t *testing.T) {
	expiresIn := time.Minute * 5
	expectedId := uuid.New()
	tokenSecret := "fadfa"
	token, err := MakeJWT(expectedId, tokenSecret, expiresIn)
	if err != nil {
		t.Errorf("there was a problem making the jwt:%v", err)
		return
	}
	_, err = ValidateJWT(token, "fadfaf")
	if err == nil {
		t.Errorf("there was a problem with the validate jwt function it should have raised error")
		return
	}
}
