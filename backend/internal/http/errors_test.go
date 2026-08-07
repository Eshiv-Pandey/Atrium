package http

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"atrium/internal/domain"
)

// The session cookie's attributes are the whole of this app's defence against
// XSS token theft and CSRF, and nothing about them is enforced by the compiler.
// Dropping HttpOnly changes no behaviour a manual test would notice, and it is
// the kind of edit that gets made while debugging and never reverted.

func setCookie(t *testing.T, secure bool) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	setSessionCookie(rec, "a-token", secure, time.Hour)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func TestSetSessionCookie(t *testing.T) {
	t.Run("flags", func(t *testing.T) {
		c := setCookie(t, true)

		if !c.HttpOnly {
			t.Error("HttpOnly is not set: JavaScript can read the session token, " +
				"which is the reason this is a cookie rather than localStorage")
		}
		if !c.Secure {
			t.Error("Secure is not set: the token can be sent over plain HTTP")
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("SameSite = %v, want Lax: this is what stops another site "+
				"making authenticated requests on the user's behalf", c.SameSite)
		}
		if c.Path != "/" {
			t.Errorf("Path = %q, want /", c.Path)
		}
		if c.Value != "a-token" {
			t.Errorf("Value = %q, want the token", c.Value)
		}
		if c.MaxAge != int(time.Hour.Seconds()) {
			t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
		}
	})

	// The __Host- prefix is enforced by the browser, not by us: it will refuse
	// a cookie of that name unless it is Secure, Path=/, and has no Domain. It
	// is what stops a compromised subdomain from overwriting the session. The
	// prefix therefore cannot be used over plain HTTP, so local development
	// gets the unprefixed name.
	t.Run("secure deployments use the __Host- prefix", func(t *testing.T) {
		if got := setCookie(t, true).Name; got != sessionCookieSecure {
			t.Errorf("cookie name = %q, want %q", got, sessionCookieSecure)
		}
		// The browser rule the prefix depends on, asserted here so a later edit
		// cannot leave the name in place while dropping what makes it valid.
		if c := setCookie(t, true); c.Domain != "" {
			t.Errorf("Domain = %q: a __Host- cookie must not set one", c.Domain)
		}
	})

	t.Run("local development uses the plain name", func(t *testing.T) {
		c := setCookie(t, false)
		if c.Name != sessionCookieInsecure {
			t.Errorf("cookie name = %q, want %q", c.Name, sessionCookieInsecure)
		}
		if c.Secure {
			t.Error("Secure is set with secureCookies=false, so the cookie will " +
				"never be sent over http://localhost and login will appear to fail")
		}
		// Everything else must survive the downgrade. Only the transport
		// changes locally, not the protection against script access.
		if !c.HttpOnly {
			t.Error("HttpOnly dropped in the insecure configuration")
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("SameSite = %v in the insecure configuration, want Lax", c.SameSite)
		}
	})
}

// TestClearSessionCookie checks that signing out actually signs the user out.
//
// A browser matches cookies for deletion on name, path, and domain. Clearing
// with attributes that differ from those used to set it creates a second,
// already-expired cookie and leaves the live session in place — a sign-out that
// reports success and does nothing.
func TestClearSessionCookie(t *testing.T) {
	for _, secure := range []bool{true, false} {
		t.Run(fmt.Sprintf("secure=%v", secure), func(t *testing.T) {
			setRec, clearRec := httptest.NewRecorder(), httptest.NewRecorder()
			setSessionCookie(setRec, "a-token", secure, time.Hour)
			clearSessionCookie(clearRec, secure)

			set := setRec.Result().Cookies()[0]
			cleared := clearRec.Result().Cookies()[0]

			if cleared.Name != set.Name {
				t.Errorf("clearing writes cookie %q but the session is %q", cleared.Name, set.Name)
			}
			if cleared.Path != set.Path {
				t.Errorf("Path = %q when clearing, %q when setting", cleared.Path, set.Path)
			}
			if cleared.Secure != set.Secure {
				t.Errorf("Secure = %v when clearing, %v when setting", cleared.Secure, set.Secure)
			}
			if cleared.MaxAge >= 0 {
				t.Errorf("MaxAge = %d, want a negative value to delete the cookie", cleared.MaxAge)
			}
			if cleared.Value != "" {
				t.Errorf("cleared cookie still carries a value: %q", cleared.Value)
			}
		})
	}
}

// Error mapping lives in exactly one function so that it cannot drift between
// endpoints. These tests are that function's contract: they are what makes the
// single mapping point worth having rather than merely tidy.

func TestStatusForError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		// 422 rather than 400: the JSON parsed and the server understood the
		// request, it just asked for something the rules forbid.
		{"validation", domain.ErrValidation, http.StatusUnprocessableEntity, CodeValidation},
		{"unauthorized", domain.ErrUnauthorized, http.StatusUnauthorized, CodeUnauthorized},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden, CodeForbidden},
		{"not found", domain.ErrNotFound, http.StatusNotFound, CodeNotFound},
		{"conflict", domain.ErrConflict, http.StatusConflict, CodeConflict},

		// Wrapped, which is how they actually arrive: the service layer adds a
		// message with %w. Matching on errors.Is rather than equality is what
		// allows that, and this row is what proves it.
		{
			"wrapped conflict",
			fmt.Errorf("%w: that slot is already booked", domain.ErrConflict),
			http.StatusConflict, CodeConflict,
		},
		{
			"doubly wrapped validation",
			fmt.Errorf("create booking: %w",
				fmt.Errorf("%w: duration too long", domain.ErrValidation)),
			http.StatusUnprocessableEntity, CodeValidation,
		},

		// Anything unrecognised is our fault until proven otherwise. Defaulting
		// to 400 would be the tempting mistake: it would blame the client for
		// our bugs and hide them from any monitoring that watches 5xx.
		{"unknown error", errors.New("connection refused"), http.StatusInternalServerError, CodeInternal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code := statusForError(tc.err)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestWriteError_DoesNotLeakInternals is the important half of error mapping.
//
// Internal errors carry SQL, table names, and connection strings. 4xx messages
// are written for the person reading them and pass through; 5xx must not.
func TestWriteError_DoesNotLeakInternals(t *testing.T) {
	rec := httptest.NewRecorder()
	leaky := errors.New(`pq: relation "bookings" does not exist (host=db user=atrium password=hunter2)`)

	writeError(rec, httptest.NewRequest(http.MethodGet, "/api/rooms", nil), leaky)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decodeError(t, rec)
	for _, secret := range []string{"password", "hunter2", "bookings", "pq:", "host="} {
		if containsFold(body.Message, secret) {
			t.Errorf("500 response leaks %q to the client: %q", secret, body.Message)
		}
	}
	if body.Code != CodeInternal {
		t.Errorf("code = %q, want %q", body.Code, CodeInternal)
	}
}

// TestWriteError_PassesThrough4xxMessages is the other half: a 409 that says
// only "Conflict" tells the member nothing. The service layer writes these
// messages for exactly this reason, and they are safe because they are about
// the request, not the system.
func TestWriteError_PassesThrough4xxMessages(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec,
		httptest.NewRequest(http.MethodPost, "/api/bookings", nil),
		fmt.Errorf("%w: that slot was taken a moment ago", domain.ErrConflict))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	body := decodeError(t, rec)
	if !containsFold(body.Message, "taken a moment ago") {
		t.Errorf("message = %q, want the service's explanation", body.Message)
	}
	// The sentinel's own name is an implementation detail. "Conflict: that slot
	// was taken" reads as a leaked internal; "That slot was taken" reads as a
	// product.
	if containsFold(body.Message, "conflict:") {
		t.Errorf("message leaks the sentinel prefix: %q", body.Message)
	}
	if body.Message[0] < 'A' || body.Message[0] > 'Z' {
		t.Errorf("message does not start with a capital: %q", body.Message)
	}
}

// TestUserMessage_BareSentinels checks the fallback wording.
//
// A bare sentinel reaches here whenever code returns domain.ErrNotFound with no
// context — common and correct. "Not found" is a usable message;
// "not found" lowercase, or the empty string, is not.
func TestUserMessage_BareSentinels(t *testing.T) {
	for _, sentinel := range []error{
		domain.ErrValidation,
		domain.ErrUnauthorized,
		domain.ErrForbidden,
		domain.ErrNotFound,
		domain.ErrConflict,
	} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			got := userMessage(sentinel)
			if got == "" {
				t.Fatal("no message for a bare sentinel")
			}
			if got == sentinel.Error() {
				t.Errorf("message is the raw sentinel %q rather than something a "+
					"member can read", got)
			}
			if got[0] < 'A' || got[0] > 'Z' {
				t.Errorf("message does not start with a capital: %q", got)
			}
		})
	}
}

// TestRecoverer turns the worst case into an ordinary one.
//
// Without it a nil dereference closes the connection with no response and no
// log line: the client sees a network error rather than a server error, and
// nothing records that it happened.
func TestRecoverer(t *testing.T) {
	rec := httptest.NewRecorder()
	Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("nil map write in the booking handler")
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rooms", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != CodeInternal {
		t.Errorf("code = %q, want %q", body.Code, CodeInternal)
	}
	// The panic value names internals and sometimes user data. It belongs in
	// the log, which Recoverer writes, and not in the response.
	if containsFold(body.Message, "nil map") || containsFold(body.Message, "booking handler") {
		t.Errorf("the panic value reached the client: %q", body.Message)
	}
}

// TestRequestID_EchoesAndGenerates covers the correlation id.
//
// A user reporting "it failed at about 3pm" is not something you can grep for.
// The id in the response header is, and honouring a client-supplied one lets a
// trace span the frontend and the API.
func TestRequestID_EchoesAndGenerates(t *testing.T) {
	t.Run("generates one when absent", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requestIDFrom(r.Context()) == "" {
				t.Error("no request id in the handler's context")
			}
		})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rooms", nil))

		if rec.Header().Get("X-Request-ID") == "" {
			t.Error("no X-Request-ID on the response")
		}
	})

	t.Run("echoes a client-supplied one", func(t *testing.T) {
		const supplied = "trace-from-the-frontend"
		r := httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
		r.Header.Set("X-Request-ID", supplied)

		rec := httptest.NewRecorder()
		var seen string
		RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = requestIDFrom(r.Context())
		})).ServeHTTP(rec, r)

		if seen != supplied {
			t.Errorf("context id = %q, want %q", seen, supplied)
		}
		if got := rec.Header().Get("X-Request-ID"); got != supplied {
			t.Errorf("response header = %q, want %q", got, supplied)
		}
	})
}

// containsFold is a case-insensitive substring check, used so an assertion
// about a leaked secret is not defeated by capitalisation.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
