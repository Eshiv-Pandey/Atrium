// Package testutil connects integration tests to a real Postgres.
//
// The rule this project is built around — that two overlapping confirmed
// bookings for one room cannot exist — is enforced by a Postgres exclusion
// constraint, not by Go. A fake store would therefore exercise the fake and
// prove nothing about the invariant, so the tests that matter run against a
// real database with the real migrations applied.
//
// Why an environment variable rather than testcontainers:
//
// docker-compose.yml already stands up the exact Postgres this project targets,
// with the two extensions the migrations need. Pointing the tests at it costs
// one variable and no dependencies. testcontainers would add a large transitive
// tree to solve a problem this repository has already solved, and would still
// require a Docker daemon to be running — so it buys convenience, not
// portability. CI gets the same database from a service container.
//
// The trade-off is real and worth stating: `go test ./...` with no variable set
// skips the integration tests instead of failing. That is deliberate, so a
// contributor without a database still gets the unit tests — but a suite that
// silently skips its most important test in CI is worse than no suite, so when
// CI is set the missing variable is a hard failure rather than a skip.
package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atrium/internal/domain"
	"atrium/internal/store"
)

// envDatabaseURL names a database the tests may freely destroy. They drop its
// schema on the first call and truncate every table between tests, so it must
// not be a database anyone cares about.
const envDatabaseURL = "TEST_DATABASE_URL"

const skipMessage = `%s not set, skipping integration test.

Start the database and point the tests at a scratch database:

    docker compose up -d postgres
    createdb -h localhost -U atrium atrium_test    # once
    export TEST_DATABASE_URL='postgres://atrium:atrium_dev_pass@localhost:5432/atrium_test?sslmode=disable'
`

var (
	once     sync.Once
	shared   *store.DB
	setupErr error
)

// DB returns a connection to the test database with migrations applied and
// every table empty.
//
// The pool is shared across the whole test binary and the data is reset per
// test, rather than the other way round: connecting is the slow part and the
// truncate is a few milliseconds. The consequence is that integration tests
// must not call t.Parallel — they share one database and would see each
// other's rows.
func DB(t *testing.T) *store.DB {
	t.Helper()

	url := os.Getenv(envDatabaseURL)
	if url == "" {
		// In CI a missing database means the suite is not running the tests it
		// claims to. Locally it means the contributor has not set one up, which
		// is fine. Same condition, opposite verdict.
		if os.Getenv("CI") != "" {
			t.Fatalf("%s must be set in CI: integration tests would otherwise skip silently", envDatabaseURL)
		}
		t.Skipf(skipMessage, envDatabaseURL)
	}

	once.Do(func() { shared, setupErr = connect(url) })
	if setupErr != nil {
		t.Fatalf("test database setup: %v", setupErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := reset(ctx, shared); err != nil {
		t.Fatalf("reset test database: %v", err)
	}
	return shared
}

func connect(url string) (*store.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := migrate(ctx, url); err != nil {
		return nil, err
	}

	db, err := store.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect to test database: %w", err)
	}
	return db, nil
}

// migrate drops the schema and replays every up migration in order.
//
// Starting from nothing on each run is what makes this re-runnable: applying
// 000001 to an already-migrated database fails on the first CREATE TABLE. The
// alternative — tracking which migrations have run — would reimplement
// golang-migrate's version table badly, and this is a database the environment
// variable already declares disposable.
func migrate(ctx context.Context, url string) error {
	dir, err := migrationsDir()
	if err != nil {
		return err
	}

	paths, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no migrations found in %s", dir)
	}
	// Lexical order is version order, which is the whole point of the numeric
	// prefix: 000001 sorts before 000002.
	sort.Strings(paths)

	// A dedicated connection in simple-protocol mode. Each migration file is a
	// single BEGIN...COMMIT block containing many statements, and the extended
	// protocol pgx uses by default permits only one statement per message.
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse %s: %w", envDatabaseURL, err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect for migrations: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))

	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`); err != nil {
		return fmt.Errorf("reset schema: %w", err)
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

// migrationsDir locates backend/migrations by walking up from the test's
// working directory, which `go test` sets to the package under test. Walking to
// the go.mod rather than hardcoding "../../migrations" means a test in any
// package finds them.
func migrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above the working directory")
		}
		dir = parent
	}
}

// reset empties every table.
//
// Truncating between tests rather than wrapping each one in a rolled-back
// transaction is not the usual choice, and it is forced by the most important
// test in this suite: the concurrency test needs N genuinely concurrent
// transactions committing against the same slot, which cannot happen inside a
// single enclosing transaction. Rather than run two isolation mechanisms and
// have to remember which test uses which, every test uses this one.
//
// CASCADE covers the foreign keys from bookings; naming all three tables
// explicitly rather than querying the catalogue means a table added later
// without a thought for tests fails loudly here instead of leaking rows.
func reset(ctx context.Context, db *store.DB) error {
	_, err := db.Pool().Exec(ctx, `TRUNCATE bookings, rooms, users RESTART IDENTITY CASCADE`)
	if err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	return nil
}

// Room inserts a room and returns it.
//
// Fixtures go in with direct SQL rather than through the service layer. A
// fixture built by the code under test can hide the bug it is meant to expose —
// if room creation and booking share a broken assumption, a test built on that
// assumption agrees with it.
func Room(t *testing.T, db *store.DB, capacity int, amenities ...string) *domain.Room {
	t.Helper()

	if amenities == nil {
		amenities = []string{}
	}

	room := &domain.Room{}
	// The name carries a uuid because rooms are unique on lower(name), and a
	// fixed name would make two rooms in one test collide.
	name := fmt.Sprintf("test-room-%s", uuid.NewString())

	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO rooms (name, capacity, amenities)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, capacity, amenities, created_at, updated_at`,
		name, capacity, amenities,
	).Scan(&room.ID, &room.Name, &room.Capacity, &room.Amenities, &room.CreatedAt, &room.UpdatedAt)
	if err != nil {
		t.Fatalf("insert room fixture: %v", err)
	}
	return room
}

// User inserts a user with the given role and returns it.
//
// The password hash is a placeholder: nothing that calls this is testing
// authentication, and running a real argon2id hash per fixture would add
// hundreds of milliseconds to a suite that creates users in loops.
func User(t *testing.T, db *store.DB, role domain.Role) *domain.User {
	t.Helper()

	user := &domain.User{}
	id := uuid.NewString()

	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash, name, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, password_hash, name, role, created_at`,
		fmt.Sprintf("test-%s@atrium.local", id),
		"not-a-real-hash",
		fmt.Sprintf("Test User %s", id[:8]),
		string(role),
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt)
	if err != nil {
		t.Fatalf("insert user fixture: %v", err)
	}
	return user
}
