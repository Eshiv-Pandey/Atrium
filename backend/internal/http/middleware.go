package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"atrium/internal/auth"
	"atrium/internal/domain"
)

// sessionCookie is the name of the cookie carrying the JWT.
//
// The __Host- prefix is enforced by browsers: a cookie with this name is only
// accepted if it is Secure, has Path=/, and has no Domain attribute. That
// makes it impossible for a subdomain to overwrite the session cookie, which
// is otherwise a real attack against cookie-based auth.
//
// The prefix requires HTTPS, so plain "session" is used when Secure cookies
// are off (local http://localhost development).
const (
	sessionCookieSecure   = "__Host-atrium_session"
	sessionCookieInsecure = "atrium_session"
)

func sessionCookieName(secure bool) string {
	if secure {
		return sessionCookieSecure
	}
	return sessionCookieInsecure
}

// contextKey is unexported so no other package can write to our context slots.
type contextKey int

const (
	ctxKeyUser contextKey = iota
	ctxKeyRequestID
)

// Identity is the authenticated caller, derived from a verified token.
type Identity struct {
	UserID uuid.UUID
	Email  string
	Role   domain.Role
}

func (i Identity) IsAdmin() bool { return i.Role == domain.RoleAdmin }

// identityFrom returns the caller's identity, if the request was authenticated.
func identityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKeyUser).(Identity)
	return id, ok
}

// mustIdentity returns the caller's identity, panicking if absent.
//
// A panic is correct here rather than an error return: this is only ever
// called from handlers mounted behind RequireAuth, so a missing identity means
// the router was wired wrong. That is a programming error that should be loud
// in tests, not a 500 in production that looks like a runtime condition.
func mustIdentity(ctx context.Context) Identity {
	id, ok := identityFrom(ctx)
	if !ok {
		panic("handler requires authentication but was not mounted behind RequireAuth")
	}
	return id
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// RequestID assigns each request an id and echoes it back, so a user reporting
// an error can be matched to the exact log line.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

// statusRecorder captures the status code for logging, since
// http.ResponseWriter does not expose what was written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logger records one structured line per request.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		// Query strings can carry filters that are useful in logs, but the
		// path alone avoids accidentally recording anything sensitive.
		slog.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

// Recoverer turns a panic into a 500 instead of a dropped connection.
//
// Without this, a nil dereference in one handler kills the connection with no
// response and no log line, and the client sees a network error rather than a
// server error.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", requestIDFrom(r.Context()),
				)
				writeJSON(w, http.StatusInternalServerError, errorEnvelope{ErrorBody{
					Code:    CodeInternal,
					Message: "Something went wrong on our end.",
				}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Authenticator verifies the session cookie and attaches the identity.
type Authenticator struct {
	tokens     *auth.TokenIssuer
	cookieName string
}

func NewAuthenticator(tokens *auth.TokenIssuer, secureCookies bool) *Authenticator {
	return &Authenticator{tokens: tokens, cookieName: sessionCookieName(secureCookies)}
}

// RequireAuth rejects requests without a valid session.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := a.identify(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUser, identity)))
	})
}

// RequireAdmin rejects requests that are not from an admin.
//
// Composed on top of RequireAuth rather than duplicating token verification,
// so there is exactly one code path that decides whether a token is valid.
func (a *Authenticator) RequireAdmin(next http.Handler) http.Handler {
	return a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mustIdentity(r.Context()).IsAdmin() {
			// 403, not 404: the caller is authenticated and the route exists.
			// Hiding admin routes from signed-in members buys nothing, since
			// the frontend bundle names them anyway.
			writeError(w, r, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// OptionalAuth attaches an identity when one is present but allows anonymous
// requests through, for endpoints that vary by caller without requiring one.
func (a *Authenticator) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if identity, err := a.identify(r); err == nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyUser, identity))
		}
		next.ServeHTTP(w, r)
	})
}

// identify extracts and verifies the caller's token.
//
// The cookie is the only accepted source. An Authorization header would be a
// second entry point that is not protected by SameSite, which is what defends
// the cookie against cross-site request forgery.
func (a *Authenticator) identify(r *http.Request) (Identity, error) {
	cookie, err := r.Cookie(a.cookieName)
	if err != nil {
		return Identity{}, domain.ErrUnauthorized
	}

	claims, err := a.tokens.Verify(cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrTokenExpired) {
			return Identity{}, errSessionExpired()
		}
		return Identity{}, domain.ErrUnauthorized
	}

	role := domain.Role(claims.Role)
	if role != domain.RoleMember && role != domain.RoleAdmin {
		// A token signed by us carrying a role we do not recognise means the
		// role set changed after it was issued. Refuse rather than guess.
		return Identity{}, domain.ErrUnauthorized
	}

	return Identity{UserID: claims.UserID, Email: claims.Email, Role: role}, nil
}

func errSessionExpired() error {
	// fmt.Errorf with %w, matching every other domain error, so writeError's
	// prefix-stripping produces a clean message. errors.Join would satisfy
	// errors.Is but render as two lines.
	return fmt.Errorf("%w: your session has expired; sign in again", domain.ErrUnauthorized)
}

// setSessionCookie writes the session cookie.
//
// HttpOnly keeps the token out of reach of JavaScript, so an XSS bug cannot
// exfiltrate it — the reason this is a cookie rather than localStorage.
// SameSite=Lax stops other sites from making authenticated requests on the
// user's behalf while still allowing top-level navigation into the app.
func setSessionCookie(w http.ResponseWriter, token string, secure bool, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(secure),
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie.
//
// Attributes must match those used when setting it, or the browser treats it
// as a different cookie and the original survives the sign-out.
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(secure),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
