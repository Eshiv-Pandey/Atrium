// Package config loads and validates runtime configuration from the
// environment.
//
// Everything is read once at startup and validated eagerly, so a
// misconfigured deployment fails immediately and loudly rather than at the
// first request that happens to need the missing value.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port        string
	DatabaseURL string

	// JWTSecret signs session tokens. There is deliberately no default: a
	// fallback secret is a production vulnerability that hides behind a
	// working local environment.
	JWTSecret []byte

	// TokenTTL bounds how long a stolen token stays useful. Short, because
	// stateless JWTs cannot be revoked before they expire.
	TokenTTL time.Duration

	// DemoLoginEnabled exposes POST /api/auth/demo-login, which signs in as a
	// seeded account without a password. Off unless explicitly enabled, so it
	// cannot become a production backdoor by omission.
	DemoLoginEnabled bool

	// SecureCookies sets the Secure flag on the session cookie. Off in local
	// development because http://localhost is not https; on everywhere else.
	SecureCookies bool
}

// Booking policy. These are constants rather than environment variables
// because they are product decisions, not deployment ones: a room that
// releases after 15 minutes in staging and 5 in production would make the
// no-show behaviour untestable. They live here so the service layer and the
// database CHECK constraints have a single documented source of truth.
const (
	// MinBookingDuration and MaxBookingDuration mirror
	// bookings_duration_within_bounds in 000001_init.up.sql. The database is
	// the enforcement point; these exist so the API can return a helpful 422
	// instead of surfacing a constraint violation.
	MinBookingDuration = 15 * time.Minute
	MaxBookingDuration = 8 * time.Hour

	// CheckInWindowBefore is how early a member may check in.
	CheckInWindowBefore = 5 * time.Minute

	// CheckInGracePeriod is how long after start_time a booking survives
	// without a check-in before it is released and its slot freed.
	CheckInGracePeriod = 15 * time.Minute

	// MaxBookingHorizon stops members reserving a room a year out.
	MaxBookingHorizon = 90 * 24 * time.Hour
)

// Load reads configuration from the environment, returning every problem it
// finds rather than only the first. A deployment with three missing variables
// should require one fix, not three deploys.
func Load() (*Config, error) {
	var problems []error

	cfg := &Config{
		Port:     envOr("PORT", "8080"),
		TokenTTL: time.Hour,
	}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("DATABASE_URL is required"))
	}

	secret := os.Getenv("JWT_SECRET")
	switch {
	case secret == "":
		problems = append(problems, errors.New("JWT_SECRET is required"))
	case len(secret) < 32:
		// HMAC-SHA256 keys shorter than the 32-byte digest reduce the
		// effective security of the signature.
		problems = append(problems, fmt.Errorf(
			"JWT_SECRET must be at least 32 bytes (got %d); generate one with: openssl rand -base64 32",
			len(secret),
		))
	default:
		cfg.JWTSecret = []byte(secret)
	}

	var err error
	if cfg.DemoLoginEnabled, err = boolEnv("DEMO_LOGIN_ENABLED", false); err != nil {
		problems = append(problems, err)
	}
	if cfg.SecureCookies, err = boolEnv("SECURE_COOKIES", false); err != nil {
		problems = append(problems, err)
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(problems...))
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean, got %q", key, raw)
	}
	return v, nil
}
