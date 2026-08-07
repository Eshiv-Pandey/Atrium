// Package store provides database access using pgx.
//
// The store layer owns SQL and nothing else: no business rules, no HTTP
// concepts. It translates Postgres error codes into sentinel errors the
// service layer can reason about without importing a driver package.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres error codes we act on.
//
// 23P01 is what the bookings_no_overlap exclusion constraint raises, and
// catching it is what makes the booking happy path a single INSERT with no
// preceding read. 23505 is a plain unique violation, which for bookings means
// an Idempotency-Key replay rather than a genuine conflict — the two must not
// be conflated, so they are separate predicates.
const (
	codeExclusionViolation  = "23P01"
	codeUniqueViolation     = "23505"
	codeCheckViolation      = "23514"
	codeForeignKeyViolation = "23503"
)

// DB wraps a pgx connection pool.
type DB struct {
	pool *pgxpool.Pool
}

// New creates a DB from a connection string and verifies connectivity.
func New(ctx context.Context, connString string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	// Bounded pool with a connection lifetime short enough that a pooler or
	// a rolling database restart does not leave us holding dead connections.
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Pool exposes the underlying pool for queries that do not need a transaction.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Close releases all connections in the pool.
func (db *DB) Close() { db.pool.Close() }

// Ping verifies that the database is reachable.
func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

// WithTx runs fn inside a transaction, committing on success and rolling back
// on error or panic.
//
// Rollback uses context.WithoutCancel: if the request context is already
// cancelled (client disconnected, deadline exceeded) a rollback issued on that
// context would itself fail, leaking the transaction until the server times it
// out. The rollback must outlive the reason we are rolling back.
func (db *DB) WithTx(ctx context.Context, fn func(pgx.Tx) error) (err error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// IsOverlapConflict reports whether err is the bookings_no_overlap exclusion
// violation — the slot was taken by a concurrent request.
func IsOverlapConflict(err error) bool {
	return pgErrCode(err) == codeExclusionViolation
}

// IsUniqueViolation reports whether err is a unique index violation.
// For bookings this means an Idempotency-Key was replayed.
func IsUniqueViolation(err error) bool {
	return pgErrCode(err) == codeUniqueViolation
}

// ConstraintName returns the name of the violated constraint, so callers can
// distinguish which unique index was hit.
func ConstraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// IsCheckViolation reports whether err violated a CHECK constraint. These
// indicate the service layer let through input its own validation should have
// caught, so they are a bug signal rather than a user-facing condition.
func IsCheckViolation(err error) bool {
	return pgErrCode(err) == codeCheckViolation
}

// IsForeignKeyViolation reports whether err referenced a row that
// does not exist (e.g. booking a room_id that was just deleted).
func IsForeignKeyViolation(err error) bool {
	return pgErrCode(err) == codeForeignKeyViolation
}

// IsNoRows reports whether a query returned no rows.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
