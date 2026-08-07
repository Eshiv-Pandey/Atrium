package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"atrium/internal/domain"
	"atrium/internal/service"
)

// AuthHandler owns the auth endpoints.
type AuthHandler struct {
	auth          *service.AuthService
	secureCookies bool
	tokenTTL      time.Duration
}

func NewAuthHandler(auth *service.AuthService, secureCookies bool, tokenTTL time.Duration) *AuthHandler {
	return &AuthHandler{auth: auth, secureCookies: secureCookies, tokenTTL: tokenTTL}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// authResponse carries the signed-in user. It deliberately does not carry the
// token.
//
// The token travels in an httpOnly cookie so JavaScript can never read it.
// Putting a copy in the response body would hand it straight back to
// JavaScript and undo that — it would sit in memory, in any error reporter's
// breadcrumbs, and in the network tab of a shared screen. Nothing needs it:
// the authenticator reads the cookie and only the cookie.
type authResponse struct {
	User userResponse `json:"user"`
}

// Register creates a member account.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid request body", domain.ErrValidation))
		return
	}

	user, token, err := h.auth.Register(r.Context(), service.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	setSessionCookie(w, token, h.secureCookies, h.tokenTTL)
	writeJSON(w, http.StatusCreated, authResponse{User: newUserResponse(user)})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login verifies credentials and issues a token.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid request body", domain.ErrValidation))
		return
	}

	user, token, err := h.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}

	setSessionCookie(w, token, h.secureCookies, h.tokenTTL)
	writeJSON(w, http.StatusOK, authResponse{User: newUserResponse(user)})
}

// Logout expires the session cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, h.secureCookies)
	writeNoContent(w)
}

type demoLoginRequest struct {
	Role string `json:"role"`
}

// DemoLogin signs in as a seeded account without a password.
//
// This endpoint is mounted only when DEMO_LOGIN_ENABLED=true, so a production
// deployment where that variable is absent or explicitly off has no such route.
func (h *AuthHandler) DemoLogin(w http.ResponseWriter, r *http.Request) {
	var req demoLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid request body", domain.ErrValidation))
		return
	}

	role := domain.Role(req.Role)
	if role != domain.RoleMember && role != domain.RoleAdmin {
		writeValidationError(w, "role must be 'member' or 'admin'", map[string]string{
			"role": "must be 'member' or 'admin'",
		})
		return
	}

	user, token, err := h.auth.DemoLogin(r.Context(), role)
	if err != nil {
		writeError(w, r, err)
		return
	}

	setSessionCookie(w, token, h.secureCookies, h.tokenTTL)
	writeJSON(w, http.StatusOK, authResponse{User: newUserResponse(user)})
}

// Me returns the authenticated caller's profile.
//
// The frontend calls this on every page load to decide whether it still has a
// session and who it belongs to. It reads through to the database rather than
// answering from the token's claims: see AuthService.Profile for why.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	id := mustIdentity(r.Context())

	user, err := h.auth.Profile(r.Context(), id.UserID)
	if err != nil {
		// A valid token for a user who no longer exists means the account was
		// deleted mid-session. Clearing the cookie stops the frontend from
		// retrying a request that can never succeed again.
		if errors.Is(err, domain.ErrNotFound) {
			clearSessionCookie(w, h.secureCookies)
			writeError(w, r, fmt.Errorf("%w: your account is no longer active", domain.ErrUnauthorized))
			return
		}
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newUserResponse(user))
}
