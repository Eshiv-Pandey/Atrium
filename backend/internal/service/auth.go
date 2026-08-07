package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"atrium/internal/auth"
	"atrium/internal/config"
	"atrium/internal/domain"
	"atrium/internal/store"
)

// AuthService handles registration, sign-in, and token issuance.
type AuthService struct {
	users  *store.UserStore
	hasher *auth.PasswordHasher
	tokens *auth.TokenIssuer
	cfg    *config.Config
}

func NewAuthService(users *store.UserStore, hasher *auth.PasswordHasher, tokens *auth.TokenIssuer, cfg *config.Config) *AuthService {
	return &AuthService{users: users, hasher: hasher, tokens: tokens, cfg: cfg}
}

// minPasswordLength is a floor, not a policy. Composition rules (a digit, a
// symbol, a capital) push people toward predictable substitutions and are no
// longer recommended by NIST; length is what actually helps.
const minPasswordLength = 8

// RegisterInput is a sign-up request.
type RegisterInput struct {
	Email    string
	Password string
	Name     string
}

// Register creates a member account and returns it with a signed token.
//
// New accounts are always members. Role is never taken from the request body:
// accepting it would let anyone register as an admin.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*domain.User, string, error) {
	email := normalizeEmail(in.Email)
	name := strings.TrimSpace(in.Name)

	if _, err := mail.ParseAddress(email); err != nil {
		return nil, "", fmt.Errorf("%w: that does not look like an email address", domain.ErrValidation)
	}
	if name == "" {
		return nil, "", fmt.Errorf("%w: name is required", domain.ErrValidation)
	}
	if len(in.Password) < minPasswordLength {
		return nil, "", fmt.Errorf("%w: password must be at least %d characters",
			domain.ErrValidation, minPasswordLength)
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}

	user, err := s.users.Create(ctx, &domain.User{
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		Role:         domain.RoleMember,
	})
	if err != nil {
		return nil, "", err
	}

	token, err := s.issue(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// Login verifies credentials and returns the user with a signed token.
//
// A wrong password and an unknown address return the same error. Any
// distinction here is an account enumeration oracle: an attacker who can tell
// them apart can discover who has an account.
func (s *AuthService) Login(ctx context.Context, email, password string) (*domain.User, string, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Hash the supplied password anyway. Returning early for an
			// unknown address makes that path measurably faster than the
			// wrong-password path, and the difference is enough to enumerate
			// accounts by timing.
			_, _ = s.hasher.Hash(password)
			return nil, "", errInvalidCredentials()
		}
		return nil, "", err
	}

	ok, err := s.hasher.Verify(password, user.PasswordHash)
	if err != nil {
		return nil, "", fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return nil, "", errInvalidCredentials()
	}

	token, err := s.issue(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// DemoLogin signs in as a seeded account without a password, so a reviewer can
// reach the product in one click.
//
// Gated on config.DemoLoginEnabled, which defaults to false: this endpoint is
// an unauthenticated way to obtain a session, and it must be impossible to
// enable by accident.
func (s *AuthService) DemoLogin(ctx context.Context, role domain.Role) (*domain.User, string, error) {
	if !s.cfg.DemoLoginEnabled {
		return nil, "", domain.ErrNotFound
	}

	email, ok := demoAccounts[role]
	if !ok {
		return nil, "", fmt.Errorf("%w: unknown demo role %q", domain.ErrValidation, role)
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, "", fmt.Errorf("%w: demo accounts are not seeded; run `go run ./cmd/seed`",
				domain.ErrNotFound)
		}
		return nil, "", err
	}

	token, err := s.issue(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// demoAccounts maps a role to its seeded address. Kept beside the seeder's
// definitions so the two cannot drift apart silently.
var demoAccounts = map[domain.Role]string{
	domain.RoleMember: "member@atrium.local",
	domain.RoleAdmin:  "admin@atrium.local",
}

// Profile loads the signed-in user by id.
//
// This reads the database rather than trusting the token's claims, even though
// the claims carry an id, an email, and a role. Two reasons. The claims have no
// name, and the frontend calls this on every page load to restore its session —
// serving from claims would mean the name appears at login and vanishes on
// refresh. And a claim is a snapshot from up to an hour ago: if an account is
// demoted from admin, a read here reflects that on the next page load instead
// of at token expiry. One primary-key lookup per page load is a fair price.
func (s *AuthService) Profile(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.users.GetByID(ctx, id)
}

func (s *AuthService) issue(u *domain.User) (string, error) {
	token, err := s.tokens.Issue(u.ID, u.Email, string(u.Role))
	if err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}
	return token, nil
}

func errInvalidCredentials() error {
	return fmt.Errorf("%w: incorrect email or password", domain.ErrUnauthorized)
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
