package auth

import (
	"testing"
	"time"
	"net/http"	
	"github.com/google/uuid"
)

func TestMakeAndVaidateJWT(t *testing.T) {
	secret := "super-secret"
	userID := uuid.New()

	token, err := MakeJWT(userID, secret, time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}
	parsedID, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("ValidateJWT failed: %v", err)
	}
	if parsedID != userID {
		t.Errorf("expected userID %v, got %v", userID, parsedID)
	}
}

func TestExpiredJWT(t *testing.T) {
	secret := "super-secret"
	userID := uuid.New()
	token, err := MakeJWT(userID, secret, -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}
	_, err = ValidateJWT(token, secret)
	if err == nil {
		t.Fatalf("expected error for expired token")
	}
}

func TestJWTWrongSecret(t *testing.T) {
	userID := uuid.New()

	token, err := MakeJWT(userID, "right-secret", time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}

	_, err = ValidateJWT(token, "wrong-secret")
	if err == nil {
		t.Fatalf("expected error for wrong secret")
	}
}

func TestGetBearerToken(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer abc123")
	token, err := GetBearerToken(headers)
	if err != nil {
		t.Fatal(err)
	}
	if token != "abc123" {
		t.Fatalf("expected abc123, got %s", token)
	}
}
