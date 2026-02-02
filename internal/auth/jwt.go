package auth

import (
	"time"
	"github.com/google/uuid"
	"github.com/golang-jwt/jwt/v5"
)

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	// make the jwt string and throw error if failed
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.RegisteredClaims{
		Issuer: "chirpy",
		IssuedAt: jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
		Subject: userID.String(),
	})
	ss, err := token.SignedString([]byte(tokenSecret))
	return ss, err
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token)(interface{}, error) {
			return []byte(tokenString), nil
		},
	)
	if err != nil {
		return uuid.Nil, err
	}
	if !token.Valid {
		return uuid.Nil, jwt.ErrTokenInvalidClaims
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}
