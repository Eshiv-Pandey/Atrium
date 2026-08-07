package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atrium/internal/domain"
)

// BookingStore owns SQL for the bookings table.
type BookingStore struct{ db *DB }

func NewBookingStore(db *DB) *BookingStore { return &BookingStore{db: db} }

const bookingColumns = `
	id, room_id, user_id, start_time, end_time, attendee_count,
	status, checked_in_at, released_at, cancelled_at, idempotency_key, created_at`

// bookingColumnsQualified is the same list under the alias `b`, for the one
// query that joins. Kept as a separate constant rather than building it with
// string replacement so both forms are greppable as literal SQL.
const bookingColumnsQualified = `
	b.id, b.room_id, b.user_id, b.start_time, b.end_time, b.attendee_count,
	b.status, b.checked_in_at, b.released_at, b.cancelled_at, b.idempotency_key, b.created_at`

func scanBooking(row pgx.Row) (*domain.Booking, error) {
	var b domain.Booking
	err := row.Scan(
		&b.ID, &b.RoomID, &b.UserID, &b.StartTime, &b.EndTime, &b.AttendeeCount,
		&b.Status, &b.CheckedInAt, &b.ReleasedAt, &b.CancelledAt, &b.IdempotencyKey, &b.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ReleaseStaleNoShows marks confirmed bookings that started more than
// gracePeriod ago and were never checked into as released, freeing their slots.
//
// This is the mechanism that makes no-show release work without a scheduler.
// Because the bookings_no_overlap exclusion constraint is predicated on
// status = 'confirmed', flipping a row to 'released' drops it out of the
// constraint's index — so a slot freed here is immediately bookable by an
// INSERT in the same transaction.
//
// It runs inside the booking transaction, scoped to the room being booked:
// sweeping every room on every booking would make each write proportional to
// the size of the whole table for no benefit, since a stale booking in another
// room cannot block this insert.
//
// The equivalent predicate appears in availability reads, so a stale booking
// never appears to occupy a slot either. Read and write are the only two
// moments the invariant can be observed, and it holds at both.
func (s *BookingStore) ReleaseStaleNoShows(ctx context.Context, tx pgx.Tx, roomID uuid.UUID, gracePeriod time.Duration) (int64, error) {
	const q = `
		UPDATE bookings
		SET status = 'released', released_at = now()
		WHERE room_id = $1
		  AND status = 'confirmed'
		  AND checked_in_at IS NULL
		  AND start_time + $2::interval < now()`

	tag, err := tx.Exec(ctx, q, roomID, gracePeriod)
	if err != nil {
		return 0, fmt.Errorf("release stale no-shows: %w", err)
	}
	return tag.RowsAffected(), nil
}

// FindByIdempotencyKey returns a booking previously created by this user with
// the given key, or domain.ErrNotFound.
//
// Replaying a key returns the original booking rather than creating a second
// one, so a double-submitted form or a retried request is harmless.
func (s *BookingStore) FindByIdempotencyKey(ctx context.Context, tx pgx.Tx, userID uuid.UUID, key string) (*domain.Booking, error) {
	const q = `SELECT ` + bookingColumns + `
		FROM bookings WHERE user_id = $1 AND idempotency_key = $2`

	b, err := scanBooking(tx.QueryRow(ctx, q, userID, key))
	if err != nil {
		if IsNoRows(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("find booking by idempotency key: %w", err)
	}
	return b, nil
}

// Insert writes a booking inside the given transaction.
//
// Returns domain.ErrConflict when the exclusion constraint rejects the row —
// the slot was taken, most interestingly by a request that committed
// microseconds earlier. There is deliberately no SELECT beforehand: checking
// availability and then inserting is a lost-update race no amount of care in
// Go can close, so the check lives in the constraint and this method simply
// reports what the database decided.
func (s *BookingStore) Insert(ctx context.Context, tx pgx.Tx, b *domain.Booking) (*domain.Booking, error) {
	const q = `
		INSERT INTO bookings (room_id, user_id, start_time, end_time, attendee_count, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + bookingColumns

	created, err := scanBooking(tx.QueryRow(ctx, q,
		b.RoomID, b.UserID, b.StartTime, b.EndTime, b.AttendeeCount, b.IdempotencyKey))
	if err != nil {
		switch {
		case IsOverlapConflict(err):
			return nil, fmt.Errorf("%w: that slot is already booked", domain.ErrConflict)
		case IsUniqueViolation(err) && ConstraintName(err) == "bookings_user_idempotency_key":
			// Concurrent replay of the same key: the caller retries the
			// lookup rather than treating this as a booking conflict.
			return nil, fmt.Errorf("%w: duplicate idempotency key", domain.ErrConflict)
		case IsForeignKeyViolation(err):
			return nil, fmt.Errorf("%w: room does not exist", domain.ErrValidation)
		}
		return nil, fmt.Errorf("insert booking: %w", err)
	}
	return created, nil
}

// GetByID returns one booking regardless of owner, or domain.ErrNotFound.
func (s *BookingStore) GetByID(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	const q = `SELECT ` + bookingColumns + ` FROM bookings WHERE id = $1`

	b, err := scanBooking(s.db.pool.QueryRow(ctx, q, id))
	if err != nil {
		if IsNoRows(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get booking: %w", err)
	}
	return b, nil
}

// Cancel marks a confirmed booking as cancelled, freeing its slot.
//
// The status check is in the WHERE clause rather than a preceding SELECT so
// the transition is atomic: two concurrent cancels cannot both succeed. The
// caller distinguishes "no such booking" from "not cancellable" by re-reading
// on a zero row count.
func (s *BookingStore) Cancel(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `
		UPDATE bookings
		SET status = 'cancelled', cancelled_at = now()
		WHERE id = $1 AND status = 'confirmed'`

	tag, err := s.db.pool.Exec(ctx, q, id)
	if err != nil {
		return false, fmt.Errorf("cancel booking: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// CheckIn records attendance, but only inside the permitted window and only
// for a confirmed booking that has not already been checked into.
//
// Every condition is expressed in SQL against now() rather than evaluated in
// Go, for two reasons: the window check and the write are then atomic (a
// booking cannot be released by a concurrent sweep between the two), and the
// database clock is the single reference for time — the application server's
// clock never enters the decision.
func (s *BookingStore) CheckIn(ctx context.Context, id uuid.UUID, before, grace time.Duration) (bool, error) {
	const q = `
		UPDATE bookings
		SET checked_in_at = now()
		WHERE id = $1
		  AND status = 'confirmed'
		  AND checked_in_at IS NULL
		  AND now() >= start_time - $2::interval
		  AND now() <= start_time + $3::interval`

	tag, err := s.db.pool.Exec(ctx, q, id, before, grace)
	if err != nil {
		return false, fmt.Errorf("check in booking: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// BookingFilter narrows a booking listing. Zero values mean "no constraint".
type BookingFilter struct {
	UserID *uuid.UUID
	RoomID *uuid.UUID
	From   *time.Time
	To     *time.Time
	Status *domain.BookingStatus

	// Limit caps the page size; the service layer clamps it to a maximum.
	Limit int

	// Cursor paginates by (start_time, id) of the last row of the previous
	// page. Keyset rather than OFFSET: with OFFSET, a booking created while
	// the user pages would shift every subsequent row and silently skip one.
	CursorStart *time.Time
	CursorID    *uuid.UUID
}

// BookingRow is a booking joined with the names a list needs to be readable.
//
// This is a read projection, deliberately distinct from domain.Booking. The
// aggregate is what the write path validates and inserts; a list is a
// different thing — the names are not part of the booking's identity and no
// write ever sets them. Keeping them out of the aggregate means no code path
// can be handed a Booking whose RoomName happens to be empty because it came
// from an INSERT rather than a SELECT.
//
// The alternative — returning bare ids and letting the client join — was
// rejected because the client cannot: there is no users endpoint, so an admin
// table would show UUIDs for the people who made the bookings, and adding one
// purely for a display join would expose the whole user list to satisfy a
// label.
type BookingRow struct {
	domain.Booking
	RoomName  string
	UserName  string
	UserEmail string
}

// List returns bookings matching the filter, newest first, plus whether more
// rows exist beyond this page.
func (s *BookingStore) List(ctx context.Context, f BookingFilter) ([]BookingRow, bool, error) {
	var (
		conds []string
		args  []any
	)

	add := func(cond string, arg any) {
		args = append(args, arg)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if f.UserID != nil {
		add("b.user_id = $%d", *f.UserID)
	}
	if f.RoomID != nil {
		add("b.room_id = $%d", *f.RoomID)
	}
	if f.Status != nil {
		add("b.status = $%d", *f.Status)
	}
	// Half-open [from, to): a booking touching the window is included, and
	// adjacent windows neither overlap nor leave a gap.
	if f.From != nil {
		add("b.end_time > $%d", *f.From)
	}
	if f.To != nil {
		add("b.start_time < $%d", *f.To)
	}

	if f.CursorStart != nil && f.CursorID != nil {
		args = append(args, *f.CursorStart, *f.CursorID)
		conds = append(conds, fmt.Sprintf(
			"(b.start_time, b.id) < ($%d, $%d)", len(args)-1, len(args)))
	}

	// Every column is table-qualified because the join brings three `id` and
	// three `created_at` columns into scope, and an unqualified reference to
	// either is an error rather than a silent wrong answer — but `name` exists
	// on both rooms and users, and that one would resolve silently.
	//
	// Both joins are inner, which is correct here rather than merely
	// convenient. room_id and user_id are NOT NULL, and the schema makes an
	// orphan unreachable from either side: room_id is ON DELETE RESTRICT, so a
	// room with bookings cannot be deleted at all, and user_id is ON DELETE
	// CASCADE, so a deleted member's bookings go with them. Either way there is
	// no booking whose room or user is missing, and an outer join would only
	// add null-handling for rows that cannot exist.
	q := `SELECT ` + bookingColumnsQualified + `, r.name, u.name, u.email
		FROM bookings b
		JOIN rooms r ON r.id = b.room_id
		JOIN users u ON u.id = b.user_id`
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, " AND ")
	}

	// Fetch one extra row to determine whether a next page exists without a
	// second COUNT query.
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1)
	q += fmt.Sprintf(` ORDER BY b.start_time DESC, b.id DESC LIMIT $%d`, len(args))

	rows, err := s.db.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, false, fmt.Errorf("list bookings: %w", err)
	}
	defer rows.Close()

	bookings := make([]BookingRow, 0, limit)
	for rows.Next() {
		var r BookingRow
		if err := rows.Scan(
			&r.ID, &r.RoomID, &r.UserID, &r.StartTime, &r.EndTime, &r.AttendeeCount,
			&r.Status, &r.CheckedInAt, &r.ReleasedAt, &r.CancelledAt, &r.IdempotencyKey, &r.CreatedAt,
			&r.RoomName, &r.UserName, &r.UserEmail,
		); err != nil {
			return nil, false, fmt.Errorf("scan booking: %w", err)
		}
		bookings = append(bookings, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate bookings: %w", err)
	}

	hasMore := len(bookings) > limit
	if hasMore {
		bookings = bookings[:limit]
	}
	return bookings, hasMore, nil
}

// BusyInterval is an occupied span of time for a room.
type BusyInterval struct {
	RoomID uuid.UUID
	Start  time.Time
	End    time.Time
}

// BusyIntervals returns confirmed bookings overlapping [from, to) for the given
// rooms, ordered so the service layer can subtract them from the bookable day
// in a single pass.
//
// Stale no-shows are excluded by the same grace-period predicate the release
// sweep uses, so a booking nobody checked into stops occupying its slot on
// read even before any write has swept it. Read and write agree without the
// read having to mutate anything.
//
// One query for all rooms rather than one per room: the browse page shows
// availability for the whole catalog at once, and per-room queries would make
// it N+1.
func (s *BookingStore) BusyIntervals(ctx context.Context, roomIDs []uuid.UUID, from, to time.Time, gracePeriod time.Duration) ([]BusyInterval, error) {
	const q = `
		SELECT room_id, start_time, end_time
		FROM bookings
		WHERE room_id = ANY($1)
		  AND status = 'confirmed'
		  AND end_time > $2
		  AND start_time < $3
		  AND (checked_in_at IS NOT NULL OR start_time + $4::interval >= now())
		ORDER BY room_id, start_time`

	rows, err := s.db.pool.Query(ctx, q, roomIDs, from, to, gracePeriod)
	if err != nil {
		return nil, fmt.Errorf("query busy intervals: %w", err)
	}
	defer rows.Close()

	intervals := make([]BusyInterval, 0)
	for rows.Next() {
		var iv BusyInterval
		if err := rows.Scan(&iv.RoomID, &iv.Start, &iv.End); err != nil {
			return nil, fmt.Errorf("scan busy interval: %w", err)
		}
		intervals = append(intervals, iv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate busy intervals: %w", err)
	}
	return intervals, nil
}

// CountFutureConfirmed reports how many confirmed bookings for a room start
// after now. Used to block deletion of a room people are relying on.
func (s *BookingStore) CountFutureConfirmed(ctx context.Context, roomID uuid.UUID) (int, error) {
	const q = `
		SELECT count(*) FROM bookings
		WHERE room_id = $1 AND status = 'confirmed' AND start_time > now()`

	var n int
	if err := s.db.pool.QueryRow(ctx, q, roomID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count future bookings: %w", err)
	}
	return n, nil
}

// RoomUtilization is aggregated booked time for one room over a window.
type RoomUtilization struct {
	RoomID       uuid.UUID
	RoomName     string
	Capacity     int
	BookedHours  float64
	BookingCount int
	NoShowCount  int
}

// Utilization aggregates booked hours per room over [from, to).
//
// Booked time is clamped to the window with GREATEST/LEAST so a booking
// straddling the boundary contributes only the part that falls inside it —
// otherwise a week's report could show more booked hours than the week
// contains. Aggregation happens in SQL rather than by loading rows into Go
// because the database can do it in one pass over an index.
//
// Cancelled bookings are excluded entirely; released ones are counted as
// no-shows but contribute no booked hours, since the room was in fact empty.
func (s *BookingStore) Utilization(ctx context.Context, from, to time.Time) ([]RoomUtilization, error) {
	const q = `
		SELECT
			r.id,
			r.name,
			r.capacity,
			COALESCE(SUM(
				CASE WHEN b.status = 'confirmed' THEN
					EXTRACT(EPOCH FROM (LEAST(b.end_time, $2) - GREATEST(b.start_time, $1))) / 3600.0
				ELSE 0 END
			), 0) AS booked_hours,
			COUNT(b.id) FILTER (WHERE b.status = 'confirmed') AS booking_count,
			COUNT(b.id) FILTER (WHERE b.status = 'released')  AS no_show_count
		FROM rooms r
		LEFT JOIN bookings b
			ON b.room_id = r.id
			AND b.status <> 'cancelled'
			AND b.end_time > $1
			AND b.start_time < $2
		GROUP BY r.id, r.name, r.capacity
		ORDER BY booked_hours DESC, r.name`

	rows, err := s.db.pool.Query(ctx, q, from, to)
	if err != nil {
		return nil, fmt.Errorf("query utilization: %w", err)
	}
	defer rows.Close()

	out := make([]RoomUtilization, 0)
	for rows.Next() {
		var u RoomUtilization
		if err := rows.Scan(&u.RoomID, &u.RoomName, &u.Capacity,
			&u.BookedHours, &u.BookingCount, &u.NoShowCount); err != nil {
			return nil, fmt.Errorf("scan utilization: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate utilization: %w", err)
	}
	return out, nil
}
