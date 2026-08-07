package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atrium/internal/config"
	"atrium/internal/domain"
	"atrium/internal/store"
)

// BookingService owns the rules about when a room may be reserved.
type BookingService struct {
	db       *store.DB
	bookings *store.BookingStore
	rooms    *store.RoomStore
}

func NewBookingService(db *store.DB, bookings *store.BookingStore, rooms *store.RoomStore) *BookingService {
	return &BookingService{db: db, bookings: bookings, rooms: rooms}
}

// CreateBookingInput is a validated request to reserve a room.
type CreateBookingInput struct {
	RoomID         uuid.UUID
	UserID         uuid.UUID
	Start          time.Time
	End            time.Time
	AttendeeCount  int
	IdempotencyKey *string
}

// Create reserves a room.
//
// The sequence inside one transaction is: replay check, release stale no-shows
// for this room, insert. There is deliberately no availability check — the
// exclusion constraint is the authority, and a SELECT here would only add a
// race window and a wasted round trip.
//
// The second return value reports whether this was an Idempotency-Key replay
// rather than a fresh reservation, so the transport can answer 200 instead of
// 201 without having to infer it from timestamps.
func (s *BookingService) Create(ctx context.Context, in CreateBookingInput) (*domain.Booking, bool, error) {
	room, err := s.rooms.GetByID(ctx, in.RoomID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, false, fmt.Errorf("%w: room does not exist", domain.ErrValidation)
		}
		return nil, false, err
	}

	if err := s.validate(in, room); err != nil {
		return nil, false, err
	}

	var (
		created  *domain.Booking
		replayed bool
	)
	err = s.db.WithTx(ctx, func(tx pgx.Tx) error {
		// Replay of a key we have already honoured returns the original
		// booking, so a double-submitted form does not double-book.
		if in.IdempotencyKey != nil {
			existing, err := s.bookings.FindByIdempotencyKey(ctx, tx, in.UserID, *in.IdempotencyKey)
			switch {
			case err == nil:
				created, replayed = existing, true
				return nil
			case !errors.Is(err, domain.ErrNotFound):
				return err
			}
		}

		// Free any slot held by a booking nobody turned up for. Scoped to
		// this room, because only this room's bookings can block this insert.
		if _, err := s.bookings.ReleaseStaleNoShows(ctx, tx, in.RoomID, config.CheckInGracePeriod); err != nil {
			return err
		}

		b, err := s.bookings.Insert(ctx, tx, &domain.Booking{
			RoomID:         in.RoomID,
			UserID:         in.UserID,
			StartTime:      in.Start,
			EndTime:        in.End,
			AttendeeCount:  in.AttendeeCount,
			IdempotencyKey: in.IdempotencyKey,
		})
		if err != nil {
			return err
		}
		created = b
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return created, replayed, nil
}

// validate applies the rules the database cannot express or cannot explain
// well. The CHECK constraints remain the backstop; these exist so a member
// gets "meetings can run at most 8 hours" instead of a constraint name.
func (s *BookingService) validate(in CreateBookingInput, room *domain.Room) error {
	if !in.End.After(in.Start) {
		return fmt.Errorf("%w: end time must be after start time", domain.ErrValidation)
	}

	d := in.End.Sub(in.Start)
	if d < config.MinBookingDuration {
		return fmt.Errorf("%w: bookings must run at least %s", domain.ErrValidation, config.MinBookingDuration)
	}
	if d > config.MaxBookingDuration {
		return fmt.Errorf("%w: bookings may run at most %s", domain.ErrValidation, config.MaxBookingDuration)
	}

	// Booking in the past is rejected, but with a small tolerance: a client
	// whose clock is a few seconds fast, or a request that spent a moment in
	// flight, should not be told its "now" is already history.
	const clockSkewTolerance = time.Minute
	if in.Start.Before(time.Now().Add(-clockSkewTolerance)) {
		return fmt.Errorf("%w: cannot book a time in the past", domain.ErrValidation)
	}

	if in.Start.After(time.Now().Add(config.MaxBookingHorizon)) {
		return fmt.Errorf("%w: cannot book more than %d days ahead",
			domain.ErrValidation, int(config.MaxBookingHorizon.Hours()/24))
	}

	if in.AttendeeCount < 1 {
		return fmt.Errorf("%w: at least one attendee is required", domain.ErrValidation)
	}
	if in.AttendeeCount > room.Capacity {
		return fmt.Errorf("%w: %s seats %d, but %d attendees were given",
			domain.ErrValidation, room.Name, room.Capacity, in.AttendeeCount)
	}

	return nil
}

// Cancel releases a booking the caller owns.
//
// A booking belonging to someone else returns ErrNotFound rather than
// ErrForbidden: telling a stranger that a booking exists but is not theirs
// leaks the existence of other people's reservations, and they have no
// legitimate way to know an id in the first place.
func (s *BookingService) Cancel(ctx context.Context, bookingID, userID uuid.UUID, isAdmin bool) error {
	b, err := s.bookings.GetByID(ctx, bookingID)
	if err != nil {
		return err
	}
	if b.UserID != userID && !isAdmin {
		return domain.ErrNotFound
	}

	// Already cancelled: succeed silently. Cancelling twice is the same
	// request expressed twice, and the caller's desired state already holds.
	if b.Status == domain.BookingStatusCancelled {
		return nil
	}
	if b.Status == domain.BookingStatusReleased {
		return fmt.Errorf("%w: this booking was already released as a no-show", domain.ErrConflict)
	}
	if !b.StartTime.After(time.Now()) {
		return fmt.Errorf("%w: a booking that has already started cannot be cancelled", domain.ErrConflict)
	}

	ok, err := s.bookings.Cancel(ctx, bookingID)
	if err != nil {
		return err
	}
	if !ok {
		// Lost a race with a concurrent cancel or release. Re-read to report
		// the reason accurately rather than guessing.
		current, err := s.bookings.GetByID(ctx, bookingID)
		if err != nil {
			return err
		}
		if current.Status == domain.BookingStatusCancelled {
			return nil
		}
		return fmt.Errorf("%w: booking is no longer cancellable", domain.ErrConflict)
	}
	return nil
}

// CheckIn records that someone turned up.
//
// Permitted from CheckInWindowBefore before the start until CheckInGracePeriod
// after it. Outside that window the booking is either not yet relevant or
// already forfeited, and in both cases the answer is 409 with a reason.
func (s *BookingService) CheckIn(ctx context.Context, bookingID, userID uuid.UUID, isAdmin bool) (*domain.Booking, error) {
	b, err := s.bookings.GetByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.UserID != userID && !isAdmin {
		return nil, domain.ErrNotFound
	}

	ok, err := s.bookings.CheckIn(ctx, bookingID, config.CheckInWindowBefore, config.CheckInGracePeriod)
	if err != nil {
		return nil, err
	}
	if ok {
		return s.bookings.GetByID(ctx, bookingID)
	}

	// The UPDATE matched nothing. Re-read and explain which condition failed,
	// rather than returning a bare conflict the member cannot act on.
	current, err := s.bookings.GetByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	return nil, checkInFailureReason(current, time.Now())
}

// checkInFailureReason turns a rejected check-in into a specific message.
// Separated from CheckIn so it can be unit tested without a database.
func checkInFailureReason(b *domain.Booking, now time.Time) error {
	switch {
	case b.Status == domain.BookingStatusCancelled:
		return fmt.Errorf("%w: this booking was cancelled", domain.ErrConflict)
	case b.Status == domain.BookingStatusReleased:
		return fmt.Errorf("%w: this booking was released after %s without a check-in",
			domain.ErrConflict, config.CheckInGracePeriod)
	case b.CheckedInAt != nil:
		return fmt.Errorf("%w: already checked in", domain.ErrConflict)
	case now.Before(b.StartTime.Add(-config.CheckInWindowBefore)):
		return fmt.Errorf("%w: check-in opens %s before the booking starts",
			domain.ErrConflict, config.CheckInWindowBefore)
	case now.After(b.StartTime.Add(config.CheckInGracePeriod)):
		// Still 'confirmed' only because no write has swept it yet; the next
		// booking attempt on this room will release it.
		return fmt.Errorf("%w: the %s check-in window has closed",
			domain.ErrConflict, config.CheckInGracePeriod)
	default:
		return fmt.Errorf("%w: booking cannot be checked in", domain.ErrConflict)
	}
}

// ListForUser returns one member's bookings.
func (s *BookingService) ListForUser(ctx context.Context, userID uuid.UUID, f store.BookingFilter) ([]store.BookingRow, bool, error) {
	f.UserID = &userID
	f.Limit = clampLimit(f.Limit)
	return s.bookings.List(ctx, f)
}

// List returns bookings across all users. Admin only; the handler enforces it.
func (s *BookingService) List(ctx context.Context, f store.BookingFilter) ([]store.BookingRow, bool, error) {
	f.Limit = clampLimit(f.Limit)
	return s.bookings.List(ctx, f)
}

// clampLimit bounds page size so a client cannot request the whole table.
func clampLimit(n int) int {
	const defaultLimit, maxLimit = 50, 200
	switch {
	case n <= 0:
		return defaultLimit
	case n > maxLimit:
		return maxLimit
	default:
		return n
	}
}
