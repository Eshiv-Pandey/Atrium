package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"atrium/internal/auth"
	"atrium/internal/domain"
)

// The authorization matrix. Every route in this API sits behind one of three
// middlewares, so testing the middlewares exhaustively covers authorization for
// the whole surface — far better value than asserting a status code per route,
// which would restate the router's wiring rather than test the decision.
//
// None of this needs a database: the identity comes from a signed token, and
// the middleware never reads a user row. That is a property of the design worth
// noticing — it is also why a stolen token cannot be revoked, which the README
// states as the trade-off it is.

const testSecret = "test-secret-not-used-anywhere-real"

func newTestAuthenticator(ttl time.Duration) *Authenticator {
	// secureCookies=false, so the cookie name is the plain one. The __Host-
	// prefixed name is exercised separately, in the cookie tests below.
	return NewAuthenticator(auth.NewTokenIssuer([]byte(testSecret), ttl), false)
}

// okHandler records that the request reached the far side of the middleware.
// Reaching the handler at all is half of what these tests assert; the other
// half is the identity that arrived with it.
func okHandler(reached *bool, seen *Identity) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		if id, ok := identityFrom(r.Context()); ok && seen != nil {
			*seen = id
		}
		w.WriteHeader(http.StatusOK)
	})
}

func requestWithCookie(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/bookings/me", nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookieInsecure, Value: token})
	}
	return r
}

// decodeError reads the error envelope, failing the test if the body is not
// one. Every 4xx this API produces shares this shape, and a handler that
// returned a bare string would break the frontend's single error path.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorBody {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response body is not an error envelope (%v): %s", err, rec.Body.String())
	}
	if env.Error.Code == "" {
		t.Errorf("error envelope has no code: %s", rec.Body.String())
	}
	if env.Error.Message == "" {
		t.Errorf("error envelope has no message: %s", rec.Body.String())
	}
	return env.Error
}

func TestRequireAuth(t *testing.T) {
	issuer := auth.NewTokenIssuer([]byte(testSecret), time.Hour)
	userID := uuid.New()

	valid, err := issuer.Issue(userID, "member@atrium.local", string(domain.RoleMember))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Signed correctly, but by someone else. This is the forgery case: if the
	// secret is not actually checked, this token is indistinguishable from a
	// real one and anybody can mint an admin session.
	forged, err := auth.NewTokenIssuer([]byte("a-different-secret"), time.Hour).
		Issue(uuid.New(), "attacker@example.com", string(domain.RoleAdmin))
	if err != nil {
		t.Fatalf("issue forged token: %v", err)
	}

	// A token carrying a role that is not in the role set. Reachable in
	// practice by rolling out a new role and then rolling it back while tokens
	// bearing it are still live.
	unknownRole, err := issuer.Issue(userID, "member@atrium.local", "superuser")
	if err != nil {
		t.Fatalf("issue unknown-role token: %v", err)
	}

	cases := []struct {
		name       string
		token      string
		wantStatus int
		wantCode   string
	}{
		{"no cookie at all", "", http.StatusUnauthorized, CodeUnauthorized},
		{"not a JWT", "definitely-not-a-token", http.StatusUnauthorized, CodeUnauthorized},
		{"structurally valid but garbage payload", "aaa.bbb.ccc", http.StatusUnauthorized, CodeUnauthorized},
		{"signed with the wrong secret", forged, http.StatusUnauthorized, CodeUnauthorized},
		{"signature stripped", stripSignature(valid), http.StatusUnauthorized, CodeUnauthorized},
		{"unrecognised role", unknownRole, http.StatusUnauthorized, CodeUnauthorized},
		{"valid", valid, http.StatusOK, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			rec := httptest.NewRecorder()
			newTestAuthenticator(time.Hour).
				RequireAuth(okHandler(&reached, nil)).
				ServeHTTP(rec, requestWithCookie(tc.token))

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if reached != (tc.wantStatus == http.StatusOK) {
				t.Errorf("handler reached = %v, want %v — the middleware let a "+
					"rejected request through, or blocked an accepted one", reached, !reached)
			}
			if tc.wantCode != "" {
				if got := decodeError(t, rec).Code; got != tc.wantCode {
					t.Errorf("error code = %q, want %q", got, tc.wantCode)
				}
			}
		})
	}
}

// TestRequireAuth_RejectsAlgNone covers the best-known JWT attack.
//
// An attacker takes a valid token, rewrites the header to {"alg":"none"}, drops
// the signature, and edits the claims to whatever they like. A verifier that
// trusts the header's algorithm accepts it. auth.Verify checks the method is
// HMAC before returning the key, which is what makes this a 401 — and this test
// exists because that check looks like a redundant type assertion to anyone
// tidying the code later.
func TestRequireAuth_RejectsAlgNone(t *testing.T) {
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, auth.Claims{
		UserID: uuid.New(),
		Email:  "attacker@example.com",
		Role:   string(domain.RoleAdmin),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build alg=none token: %v", err)
	}

	var reached bool
	rec := httptest.NewRecorder()
	newTestAuthenticator(time.Hour).
		RequireAdmin(okHandler(&reached, nil)).
		ServeHTTP(rec, requestWithCookie(unsigned))

	if reached {
		t.Fatal("an unsigned alg=none token was accepted as an admin session")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestRequireAuth_ExpiredTokenIsDistinguishable checks that an expired session
// says so.
//
// Both cases are 401, so the status alone cannot drive the UI. "Your session
// expired, sign in again" and "you are not signed in" call for different
// wording, and the frontend has only the message to tell them apart.
func TestRequireAuth_ExpiredTokenIsDistinguishable(t *testing.T) {
	// A negative TTL puts `exp` in the past at the moment of issue — an expired
	// token without making the test sleep.
	expired, err := auth.NewTokenIssuer([]byte(testSecret), -time.Hour).
		Issue(uuid.New(), "member@atrium.local", string(domain.RoleMember))
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}

	rec := httptest.NewRecorder()
	var reached bool
	newTestAuthenticator(time.Hour).
		RequireAuth(okHandler(&reached, nil)).
		ServeHTTP(rec, requestWithCookie(expired))

	if reached {
		t.Fatal("an expired token was accepted")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	body := decodeError(t, rec)
	if body.Message == defaultMessages[domain.ErrUnauthorized.Error()] {
		t.Errorf("expired session returns the generic message %q, so the client "+
			"cannot tell it apart from never having signed in", body.Message)
	}
}

// TestRequireAuth_AttachesIdentity checks that the claims arrive at the handler
// intact.
//
// Handlers scope every query by this UserID — "my bookings", "cancel mine".
// A middleware that authenticated correctly but attached the wrong id would
// pass every status-code assertion above and hand one member another's data.
func TestRequireAuth_AttachesIdentity(t *testing.T) {
	issuer := auth.NewTokenIssuer([]byte(testSecret), time.Hour)
	userID := uuid.New()

	token, err := issuer.Issue(userID, "admin@atrium.local", string(domain.RoleAdmin))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	var (
		reached bool
		seen    Identity
	)
	rec := httptest.NewRecorder()
	newTestAuthenticator(time.Hour).
		RequireAuth(okHandler(&reached, &seen)).
		ServeHTTP(rec, requestWithCookie(token))

	if !reached {
		t.Fatalf("valid token rejected: %s", rec.Body.String())
	}
	if seen.UserID != userID {
		t.Errorf("UserID = %s, want %s", seen.UserID, userID)
	}
	if seen.Email != "admin@atrium.local" {
		t.Errorf("Email = %q, want admin@atrium.local", seen.Email)
	}
	if seen.Role != domain.RoleAdmin {
		t.Errorf("Role = %q, want admin", seen.Role)
	}
	if !seen.IsAdmin() {
		t.Error("IsAdmin() = false for an admin token")
	}
}

// TestRequireAdmin is the privilege-escalation matrix.
//
// The distinction between the anonymous and member rows is deliberate: an
// anonymous caller gets 401 because signing in might grant access, and a
// signed-in member gets 403 because it will not. Collapsing both to one status
// would make the frontend either offer a pointless login prompt or hide a
// recoverable one.
func TestRequireAdmin(t *testing.T) {
	issuer := auth.NewTokenIssuer([]byte(testSecret), time.Hour)

	member, err := issuer.Issue(uuid.New(), "member@atrium.local", string(domain.RoleMember))
	if err != nil {
		t.Fatalf("issue member token: %v", err)
	}
	admin, err := issuer.Issue(uuid.New(), "admin@atrium.local", string(domain.RoleAdmin))
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	cases := []struct {
		name        string
		token       string
		wantStatus  int
		wantCode    string
		wantReached bool
	}{
		{"anonymous", "", http.StatusUnauthorized, CodeUnauthorized, false},
		{"member", member, http.StatusForbidden, CodeForbidden, false},
		{"admin", admin, http.StatusOK, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			rec := httptest.NewRecorder()
			newTestAuthenticator(time.Hour).
				RequireAdmin(okHandler(&reached, nil)).
				ServeHTTP(rec, requestWithCookie(tc.token))

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if reached != tc.wantReached {
				t.Errorf("admin handler reached = %v, want %v", reached, tc.wantReached)
			}
			if tc.wantCode != "" {
				if got := decodeError(t, rec).Code; got != tc.wantCode {
					t.Errorf("error code = %q, want %q", got, tc.wantCode)
				}
			}
		})
	}
}

// TestOptionalAuth checks the middleware that must never reject.
//
// It is the opposite failure mode from the two above: the bug to guard against
// is not letting someone through, it is turning a browsing visitor into a 401.
// An invalid token has to be treated as no token, because a stale cookie should
// not stop an anonymous visitor from seeing the room catalog.
func TestOptionalAuth(t *testing.T) {
	issuer := auth.NewTokenIssuer([]byte(testSecret), time.Hour)
	userID := uuid.New()

	valid, err := issuer.Issue(userID, "member@atrium.local", string(domain.RoleMember))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	expired, err := auth.NewTokenIssuer([]byte(testSecret), -time.Hour).
		Issue(userID, "member@atrium.local", string(domain.RoleMember))
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}

	cases := []struct {
		name         string
		token        string
		wantIdentity bool
	}{
		{"no cookie", "", false},
		{"garbage cookie", "not-a-token", false},
		{"expired token", expired, false},
		{"valid token", valid, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				reached bool
				seen    Identity
			)
			rec := httptest.NewRecorder()
			newTestAuthenticator(time.Hour).
				OptionalAuth(okHandler(&reached, &seen)).
				ServeHTTP(rec, requestWithCookie(tc.token))

			if !reached {
				t.Fatalf("OptionalAuth blocked a request (status %d): it must never reject", rec.Code)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}

			gotIdentity := seen.UserID != uuid.Nil
			if gotIdentity != tc.wantIdentity {
				t.Errorf("identity attached = %v, want %v", gotIdentity, tc.wantIdentity)
			}
		})
	}
}

// stripSignature removes a JWT's signature while leaving header and payload
// intact — the shape an attacker produces by truncating a token they have seen.
func stripSignature(token string) string {
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			return token[:i+1]
		}
	}
	return token
}
