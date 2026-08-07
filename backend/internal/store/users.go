package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atrium/internal/domain"
)

// UserStore owns SQL for the users table.
type UserStore struct{ db *DB }

func NewUserStore(db *DB) *UserStore { return &UserStore{db: db} }

const userColumns = `id, email, password_hash, name, role, created_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Create inserts a user. Returns domain.ErrConflict if the email is taken.
//
// Email uniqueness is enforced by the users_email_lower_key index rather than
// by a SELECT beforehand, for the same reason bookings rely on their exclusion
// constraint: a check-then-insert leaves a window in which two concurrent
// registrations both see the address as free.
func (s *UserStore) Create(ctx context.Context, u *domain.User) (*domain.User, error) {
	const q = `
		INSERT INTO users (email, password_hash, name, role)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + userColumns

	created, err := scanUser(s.db.pool.QueryRow(ctx, q, u.Email, u.PasswordHash, u.Name, u.Role))
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: email already registered", domain.ErrConflict)
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return created, nil
}

// GetByEmail looks a user up by email, case-insensitively to match the
// unique index. Returns domain.ErrNotFound if absent.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	const q = `SELECT ` + userColumns + ` FROM users WHERE lower(email) = lower($1)`

	u, err := scanUser(s.db.pool.QueryRow(ctx, q, strings.TrimSpace(email)))
	if err != nil {
		if IsNoRows(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

// GetByID looks a user up by id. Returns domain.ErrNotFound if absent.
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	const q = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	u, err := scanUser(s.db.pool.QueryRow(ctx, q, id))
	if err != nil {
		if IsNoRows(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}
