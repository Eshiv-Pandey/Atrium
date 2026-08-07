package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"atrium/internal/domain"
	"atrium/internal/store"
)

// AdminService owns admin-only operations.
type AdminService struct {
	rooms    *store.RoomStore
	bookings *store.BookingStore
}

func NewAdminService(rooms *store.RoomStore, bookings *store.BookingStore) *AdminService {
	return &AdminService{rooms: rooms, bookings: bookings}
}

// CreateRoom adds a room to the catalogue.
func (s *AdminService) CreateRoom(ctx context.Context, r *domain.Room) (*domain.Room, error) {
	return s.rooms.Create(ctx, r)
}

// UpdateRoom applies a partial update.
func (s *AdminService) UpdateRoom(ctx context.Context, id uuid.UUID, u store.RoomUpdate) (*domain.Room, error) {
	return s.rooms.Update(ctx, id, u)
}

// DeleteRoom removes a room from the catalogue.
//
// Returns domain.ErrConflict with a list of future bookings if any exist: an
// admin must cancel those explicitly before the room can be removed, so they
// see what they are about to break and members get notified of the cancellation.
func (s *AdminService) DeleteRoom(ctx context.Context, id uuid.UUID) error {
	count, err := s.bookings.CountFutureConfirmed(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: %d future booking(s) would be lost; cancel them first", domain.ErrConflict, count)
	}

	// The ON DELETE RESTRICT foreign key backstops this: if the count check is
	// ever bypassed, Postgres refuses rather than silently destroying bookings.
	return s.rooms.Delete(ctx, id)
}

// Utilization aggregates booked hours per room over a window.
func (s *AdminService) Utilization(ctx context.Context, window Interval) ([]store.RoomUtilization, error) {
	if err := validateWindow(window); err != nil {
		return nil, err
	}
	return s.bookings.Utilization(ctx, window.Start, window.End)
}
