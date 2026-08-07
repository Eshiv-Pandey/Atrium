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
	"math/rand/v2"
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

// Transient failure codes. Unlike the constraint violations above, these carry
// no information about the request — they say the transaction was interrupted,
// not that it asked for something impossible. See WithTx.
const (
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
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

// Retry budget and backoff shape. Small, because with the room lock in place a
// retryable abort is a rare accident rather than the expected path — the wide
// windows a contended exclusion constraint would need are what BookingStore.
// LockRoom exists to make unnecessary.
const (
	maxTxAttempts  = 3
	txBackoffBase  = 20 * time.Millisecond
	txBackoffLimit = 200 * time.Millisecond
)

// WithTx runs fn inside a transaction, committing on success and rolling back
// on error or panic. A transaction aborted by a transient failure is retried
// from the beginning, up to maxTxAttempts.
//
// The retry is a backstop for incidental contention — two writers touching the
// same rows in opposite orders — and nothing more. It is explicitly not how
// booking concurrency is handled: retrying into a contended exclusion
// constraint makes things worse, because each new attempt joins the pileup it
// is waiting on. store.LockRoom has the measurements.
//
// Only 40001 and 40P01 qualify. Both mean the transaction was interrupted and
// asked nothing impossible, so a second attempt can legitimately differ. A
// constraint violation is the opposite: it is an answer, it would repeat
// identically, and retrying it would only delay the 409 the caller is owed.
//
// Retrying is safe because the aborted transaction rolled back in full: there
// is no partial write for a second attempt to trip over, and fn re-runs from a
// clean slate.
func (db *DB) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = db.runTx(ctx, fn)
		if !isRetryableTxError(err) || attempt == maxTxAttempts {
			return err
		}

		// Backoff before the next attempt. Full jitter — a uniform draw over the
		// whole window rather than a fixed delay plus noise — because aborted
		// transactions are released at the same instant and a fixed delay would
		// put them all back on the wire together.
		select {
		case <-ctx.Done():
			// The caller is gone. Returning the database's error rather than
			// ctx.Err() keeps the reason for the failure, which is the more
			// useful thing in a log.
			return err
		case <-time.After(txBackoff(attempt)):
		}
	}
}

// txBackoff returns a uniform random delay in [0, window), where window doubles
// per attempt up to txBackoffLimit.
func txBackoff(attempt int) time.Duration {
	window := txBackoffBase << (attempt - 1)
	if window > txBackoffLimit || window <= 0 { // <= 0 guards the shift overflowing
		window = txBackoffLimit
	}
	return time.Duration(rand.N(int64(window)))
}

// runTx is one attempt: begin, run, commit, rolling back on error or panic.
//
// Rollback uses context.WithoutCancel: if the request context is already
// cancelled (client disconnected, deadline exceeded) a rollback issued on that
// context would itself fail, leaking the transaction until the server times it
// out. The rollback must outlive the reason we are rolling back.
func (db *DB) runTx(ctx context.Context, fn func(pgx.Tx) error) (err error) {
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

// isRetryableTxError reports whether err aborted the transaction for a reason
// that a second attempt could resolve. Deliberately narrow: only the two codes
// that mean "interrupted, try again", never a constraint violation, which would
// fail identically every time and whose repetition would only delay the 409.
func isRetryableTxError(err error) bool {
	switch pgErrCode(err) {
	case codeSerializationFailure, codeDeadlockDetected:
		return true
	}
	return false
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
