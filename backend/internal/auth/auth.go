// Package auth provides password hashing and JWT token operations.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
)

// PasswordHasher hashes and verifies passwords using argon2id.
type PasswordHasher struct {
	params *argon2id.Params
}

func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		params: &argon2id.Params{
			Memory:      64 * 1024, // 64 MiB
			Iterations:  3,
			Parallelism: 2,
			SaltLength:  16,
			KeyLength:   32,
		},
	}
}

// Hash returns an argon2id hash of the password, encoded as a string that
// embeds its parameters and salt. The hash can be stored directly in the
// database.
func (h *PasswordHasher) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, h.params)
}

// Verify checks whether the plaintext password matches the stored hash.
func (h *PasswordHasher) Verify(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}

// TokenIssuer issues and verifies JWT tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenIssuer(secret []byte, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{
		secret: secret,
		ttl:    ttl,
	}
}

// Claims embedded in the JWT payload.
type Claims struct {
	UserID uuid.UUID `json:"sub"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// Issue creates a signed JWT for the given user.
func (ti *TokenIssuer) Issue(userID uuid.UUID, email, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ti.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        generateJTI(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(ti.secret)
}

// Verify parses and validates the token, returning its claims.
func (ti *TokenIssuer) Verify(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method is what we expect.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return ti.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// generateJTI creates a random JWT ID for replay detection.
func generateJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a UUID if crypto/rand fails, which it should not.
		return uuid.New().String()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
