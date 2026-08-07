package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"atrium/internal/config"
	"atrium/internal/domain"
	"atrium/internal/store"
)

// AvailabilityService answers "what can I book, and when".
type AvailabilityService struct {
	rooms    *store.RoomStore
	bookings *store.BookingStore
}

func NewAvailabilityService(rooms *store.RoomStore, bookings *store.BookingStore) *AvailabilityService {
	return &AvailabilityService{rooms: rooms, bookings: bookings}
}

// RoomAvailability is a room plus, when a window was supplied, whether it is
// free for the whole of it.
type RoomAvailability struct {
	Room domain.Room

	// Available is nil when the caller asked for no particular window, which
	// distinguishes "not asked" from "asked, and the answer is no". A bare
	// bool would render an unqueried room as unavailable.
	Available *bool
}

// ListRoomsInput filters the catalogue and optionally asks about a window.
type ListRoomsInput struct {
	MinCapacity int
	Amenities   []string
	Window      *Interval
}

// ListRooms returns the catalogue, annotated with availability when a window
// is given.
//
// Busy intervals for every matching room are fetched in one query and grouped
// in memory, rather than querying per room: the browse page asks about the
// whole catalogue at once, and the per-room form would be N+1.
func (s *AvailabilityService) ListRooms(ctx context.Context, in ListRoomsInput) ([]RoomAvailability, error) {
	rooms, err := s.rooms.List(ctx, store.RoomFilter{
		MinCapacity: in.MinCapacity,
		Amenities:   in.Amenities,
	})
	if err != nil {
		return nil, err
	}

	out := make([]RoomAvailability, len(rooms))
	for i, r := range rooms {
		out[i] = RoomAvailability{Room: r}
	}

	if in.Window == nil || len(rooms) == 0 {
		return out, nil
	}
	if err := validateWindow(*in.Window); err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, len(rooms))
	for i, r := range rooms {
		ids[i] = r.ID
	}

	busy, err := s.bookings.BusyIntervals(ctx, ids, in.Window.Start, in.Window.End, config.CheckInGracePeriod)
	if err != nil {
		return nil, err
	}

	byRoom := make(map[uuid.UUID][]Interval, len(rooms))
	for _, b := range busy {
		byRoom[b.RoomID] = append(byRoom[b.RoomID], Interval{Start: b.Start, End: b.End})
	}

	for i := range out {
		free := IsFree(*in.Window, byRoom[out[i].Room.ID])
		out[i].Available = &free
	}
	return out, nil
}

// DayAvailability describes one room's occupancy across a window.
type DayAvailability struct {
	Room  domain.Room
	Busy  []Interval
	Free  []Interval
	Start time.Time
	End   time.Time
}

// GetDayAvailability returns the busy and free spans for one room over a
// window, for the timeline UI.
//
// Free slots shorter than the minimum bookable duration are omitted: a
// twelve-minute gap between meetings cannot be reserved, so offering it would
// invite a request the service is bound to reject.
func (s *AvailabilityService) GetDayAvailability(ctx context.Context, roomID uuid.UUID, window Interval) (*DayAvailability, error) {
	if err := validateWindow(window); err != nil {
		return nil, err
	}

	room, err := s.rooms.GetByID(ctx, roomID)
	if err != nil {
		return nil, err
	}

	busy, err := s.bookings.BusyIntervals(ctx, []uuid.UUID{roomID}, window.Start, window.End, config.CheckInGracePeriod)
	if err != nil {
		return nil, err
	}

	intervals := make([]Interval, 0, len(busy))
	for _, b := range busy {
		// Clamp to the requested window so a booking straddling midnight does
		// not render as extending past the edge of the day being shown.
		intervals = append(intervals, Interval{
			Start: maxTime(b.Start, window.Start),
			End:   minTime(b.End, window.End),
		})
	}
	merged := MergeIntervals(intervals)

	return &DayAvailability{
		Room:  *room,
		Busy:  merged,
		Free:  FreeSlots(window, merged, config.MinBookingDuration),
		Start: window.Start,
		End:   window.End,
	}, nil
}

// maxWindow bounds how much history or future a single availability query may
// span, so one request cannot ask the database to scan a year.
const maxWindow = 31 * 24 * time.Hour

func validateWindow(w Interval) error {
	if w.IsEmpty() {
		return fmt.Errorf("%w: end must be after start", domain.ErrValidation)
	}
	if w.Duration() > maxWindow {
		return fmt.Errorf("%w: window may span at most %d days",
			domain.ErrValidation, int(maxWindow.Hours()/24))
	}
	return nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
