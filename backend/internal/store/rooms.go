package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atrium/internal/domain"
)

// RoomStore owns SQL for the rooms table.
type RoomStore struct{ db *DB }

func NewRoomStore(db *DB) *RoomStore { return &RoomStore{db: db} }

const roomColumns = `id, name, capacity, amenities, created_at, updated_at`

func scanRoom(row pgx.Row) (*domain.Room, error) {
	var r domain.Room
	err := row.Scan(&r.ID, &r.Name, &r.Capacity, &r.Amenities, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// RoomFilter narrows a room listing.
type RoomFilter struct {
	MinCapacity int      // 0 means no lower bound
	Amenities   []string // room must have all of these
}

// List returns rooms matching the filter, ordered by capacity then name so the
// browse page has a stable, sensible default ordering.
//
// The amenities filter uses the @> containment operator, which is what the
// rooms_amenities_idx GIN index accelerates. Semantics are "has all of",
// not "has any of": asking for a whiteboard and a TV should not return a room
// with only a whiteboard.
func (s *RoomStore) List(ctx context.Context, f RoomFilter) ([]domain.Room, error) {
	var (
		conds []string
		args  []any
	)

	if f.MinCapacity > 0 {
		args = append(args, f.MinCapacity)
		conds = append(conds, fmt.Sprintf("capacity >= $%d", len(args)))
	}
	if len(f.Amenities) > 0 {
		args = append(args, f.Amenities)
		conds = append(conds, fmt.Sprintf("amenities @> $%d", len(args)))
	}

	q := `SELECT ` + roomColumns + ` FROM rooms`
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, " AND ")
	}
	q += ` ORDER BY capacity, name`

	rows, err := s.db.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	rooms := make([]domain.Room, 0)
	for rows.Next() {
		var r domain.Room
		if err := rows.Scan(&r.ID, &r.Name, &r.Capacity, &r.Amenities, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rooms: %w", err)
	}
	return rooms, nil
}

// GetByID returns one room, or domain.ErrNotFound.
func (s *RoomStore) GetByID(ctx context.Context, id uuid.UUID) (*domain.Room, error) {
	const q = `SELECT ` + roomColumns + ` FROM rooms WHERE id = $1`

	r, err := scanRoom(s.db.pool.QueryRow(ctx, q, id))
	if err != nil {
		if IsNoRows(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	return r, nil
}

// Create inserts a room. Returns domain.ErrConflict if the name is taken.
func (s *RoomStore) Create(ctx context.Context, r *domain.Room) (*domain.Room, error) {
	const q = `
		INSERT INTO rooms (name, capacity, amenities)
		VALUES ($1, $2, $3)
		RETURNING ` + roomColumns

	// A nil slice would insert NULL, violating the NOT NULL on amenities;
	// the column's zero value is an empty array.
	amenities := r.Amenities
	if amenities == nil {
		amenities = []string{}
	}

	created, err := scanRoom(s.db.pool.QueryRow(ctx, q, r.Name, r.Capacity, amenities))
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a room with that name already exists", domain.ErrConflict)
		}
		return nil, fmt.Errorf("create room: %w", err)
	}
	return created, nil
}

// RoomUpdate carries a partial update. Nil fields are left unchanged, which is
// what makes the HTTP layer's PATCH semantics work: omitting a field means
// "leave it alone", not "set it to zero".
type RoomUpdate struct {
	Name      *string
	Capacity  *int
	Amenities *[]string
}

// Update applies a partial update. Returns domain.ErrNotFound if the room does
// not exist, or domain.ErrConflict if the new name is taken.
func (s *RoomStore) Update(ctx context.Context, id uuid.UUID, u RoomUpdate) (*domain.Room, error) {
	var (
		sets = []string{"updated_at = now()"}
		args []any
	)

	if u.Name != nil {
		args = append(args, *u.Name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if u.Capacity != nil {
		args = append(args, *u.Capacity)
		sets = append(sets, fmt.Sprintf("capacity = $%d", len(args)))
	}
	if u.Amenities != nil {
		amenities := *u.Amenities
		if amenities == nil {
			amenities = []string{}
		}
		args = append(args, amenities)
		sets = append(sets, fmt.Sprintf("amenities = $%d", len(args)))
	}

	// Nothing to change: return the current row rather than issuing a
	// pointless write, so an empty PATCH is a no-op rather than an error.
	if len(args) == 0 {
		return s.GetByID(ctx, id)
	}

	args = append(args, id)
	q := fmt.Sprintf(
		`UPDATE rooms SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(sets, ", "), len(args), roomColumns,
	)

	r, err := scanRoom(s.db.pool.QueryRow(ctx, q, args...))
	if err != nil {
		if IsNoRows(err) {
			return nil, domain.ErrNotFound
		}
		if IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a room with that name already exists", domain.ErrConflict)
		}
		return nil, fmt.Errorf("update room: %w", err)
	}
	return r, nil
}

// Delete removes a room. Returns domain.ErrNotFound if it does not exist.
//
// The service layer checks for future confirmed bookings first and returns a
// 409 listing them. The ON DELETE RESTRICT foreign key is the backstop: if
// that check is ever bypassed, Postgres refuses rather than silently
// destroying reservations people are relying on.
func (s *RoomStore) Delete(ctx context.Context, id uuid.UUID) error {
	const q = `DELETE FROM rooms WHERE id = $1`

	tag, err := s.db.pool.Exec(ctx, q, id)
	if err != nil {
		if IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: room has bookings", domain.ErrConflict)
		}
		return fmt.Errorf("delete room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
