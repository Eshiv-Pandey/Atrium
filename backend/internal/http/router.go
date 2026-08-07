package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"atrium/internal/auth"
	"atrium/internal/config"
	"atrium/internal/service"
	"atrium/internal/store"
)

// Deps are everything the router needs to build its handlers.
//
// Passed as a struct rather than fifteen positional arguments so adding a
// dependency does not silently reorder an existing call site.
type Deps struct {
	Config       *config.Config
	DB           *store.DB
	Tokens       *auth.TokenIssuer
	Auth         *service.AuthService
	Availability *service.AvailabilityService
	Bookings     *service.BookingService
	Admin        *service.AdminService
}

// maxRequestBody caps how much a client may send.
//
// Every request body here is a small JSON object; without a cap, a single
// client can make the server buffer as much memory as it feels like. 1 MiB is
// far more than any legitimate request needs.
const maxRequestBody = 1 << 20

// NewRouter assembles the API.
//
// The whole authorization matrix is visible in this one function: which routes
// are public, which need a session, which need an admin. That is deliberate —
// if the answer were spread across handlers, "can a member reach this?" would
// require reading every file to answer, and the day someone forgets a check is
// the day it becomes a vulnerability rather than a bug.
func NewRouter(d Deps) http.Handler {
	authn := NewAuthenticator(d.Tokens, d.Config.SecureCookies)

	authH := NewAuthHandler(d.Auth, d.Config.SecureCookies, d.Config.TokenTTL)
	roomsH := NewRoomsHandler(d.Availability)
	bookingsH := NewBookingsHandler(d.Bookings)
	adminH := NewAdminHandler(d.Admin, d.Bookings)
	healthH := NewHealthHandler(d.DB)

	r := chi.NewRouter()

	// Recoverer sits outermost of the two error-handling middlewares so it also
	// catches a panic raised inside Logger, and RequestID comes first so every
	// log line — including a panic — carries the id echoed to the client.
	r.Use(RequestID)
	r.Use(Recoverer)
	r.Use(Logger)
	r.Use(MaxBodyBytes(maxRequestBody))

	// chi's defaults answer in plain text, which would give the frontend one
	// error shape for handled errors and another for routing misses.
	r.NotFound(notFound)
	r.MethodNotAllowed(methodNotAllowed)

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", healthH.Check)

		// Public: no session required, because these are how you get one.
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", authH.Register)
			r.Post("/login", authH.Login)
			r.Post("/logout", authH.Logout)

			// Mounted only when explicitly enabled. A route that does not exist
			// cannot be reached by a misconfigured proxy or a forgotten flag
			// check inside the handler — the absence is the enforcement.
			if d.Config.DemoLoginEnabled {
				r.Post("/demo-login", authH.DemoLogin)
			}

			r.With(authn.RequireAuth).Get("/me", authH.Me)
		})

		// Members: any authenticated user.
		r.Group(func(r chi.Router) {
			r.Use(authn.RequireAuth)

			r.Get("/rooms", roomsH.ListRooms)
			r.Get("/rooms/{id}/availability", roomsH.GetDayAvailability)

			r.Post("/bookings", bookingsH.Create)
			r.Get("/bookings/me", bookingsH.ListMine)
			r.Post("/bookings/{id}/check-in", bookingsH.CheckIn)
			r.Delete("/bookings/{id}", bookingsH.Cancel)
		})

		// Admins only.
		//
		// Note POST/PATCH/DELETE /rooms live here while GET /rooms is above:
		// same path, different methods, different authority. Reading the
		// catalogue is what members do; changing it is not.
		r.Group(func(r chi.Router) {
			r.Use(authn.RequireAdmin)

			r.Post("/rooms", adminH.CreateRoom)
			r.Patch("/rooms/{id}", adminH.UpdateRoom)
			r.Delete("/rooms/{id}", adminH.DeleteRoom)

			r.Get("/bookings", adminH.ListBookings)
			r.Get("/admin/utilization", adminH.Utilization)
		})
	})

	return r
}

// MaxBodyBytes limits the size of a request body.
//
// MaxBytesReader makes the read fail past the limit rather than buffering the
// whole thing first, so an oversized upload costs the server the limit and not
// the payload.
func MaxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, errorEnvelope{ErrorBody{
		Code:    CodeNotFound,
		Message: "No such endpoint.",
	}})
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusMethodNotAllowed, errorEnvelope{ErrorBody{
		Code:    CodeBadRequest,
		Message: "That method is not allowed on this endpoint.",
	}})
}
