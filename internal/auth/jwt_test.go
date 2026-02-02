package auth

import (
	"testing"
	"time"
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
