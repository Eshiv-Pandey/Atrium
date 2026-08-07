// Command server runs the Atrium API.
//
// There is exactly one entrypoint. Docker Compose runs this binary, the
// integration tests mount the same router, and a hosted deployment runs the
// same server on whatever $PORT it injects. A second transport for a second
// environment would be a second place for behaviour to drift.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"atrium/internal/auth"
	"atrium/internal/config"
	apihttp "atrium/internal/http"
	"atrium/internal/service"
	"atrium/internal/store"
)

func main() {
	// A self-probe mode, so the container healthcheck can be the binary that is
	// already in the image. The final image is distroless: it has no shell, no
	// curl, and no wget, and adding one purely to answer a healthcheck would
	// undo the reason for choosing distroless in the first place.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server and exit")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if *healthcheck {
		if err := probe(); err != nil {
			slog.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// probe requests the server's own health endpoint over the loopback interface.
func probe() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

func run() error {
	// Signals are trapped before anything else starts, so a Ctrl-C during a
	// slow database connect still shuts down cleanly instead of being ignored
	// until the server is listening.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		// Configuration problems are reported all at once and the process
		// refuses to start. A server that boots with a missing JWT secret and
		// discovers it on the first login is a worse outcome than one that
		// never boots.
		return err
	}

	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	db, err := store.New(dbCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	var (
		users    = store.NewUserStore(db)
		rooms    = store.NewRoomStore(db)
		bookings = store.NewBookingStore(db)

		hasher = auth.NewPasswordHasher()
		tokens = auth.NewTokenIssuer(cfg.JWTSecret, cfg.TokenTTL)
	)

	router := apihttp.NewRouter(apihttp.Deps{
		Config:       cfg,
		DB:           db,
		Tokens:       tokens,
		Auth:         service.NewAuthService(users, hasher, tokens, cfg),
		Availability: service.NewAvailabilityService(rooms, bookings),
		Bookings:     service.NewBookingService(db, bookings, rooms),
		Admin:        service.NewAdminService(rooms, bookings),
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,

		// Timeouts are set explicitly because net/http's zero values mean "no
		// timeout": a client that opens a connection and never sends a request
		// would otherwise hold it open forever, and enough of those exhaust the
		// server. ReadHeaderTimeout in particular is what closes Slowloris.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening",
			"port", cfg.Port,
			"demo_login_enabled", cfg.DemoLoginEnabled,
			"secure_cookies", cfg.SecureCookies,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	// Graceful shutdown: stop accepting new connections, let in-flight requests
	// finish. This matters more than it looks — a booking transaction killed
	// mid-commit during a deploy is exactly the kind of thing that produces a
	// row nobody can explain later.
	//
	// context.Background rather than ctx: ctx is already cancelled, and a
	// shutdown on a cancelled context returns immediately, which is the
	// opposite of graceful.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelShutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	slog.Info("shutdown complete")
	return nil
}
