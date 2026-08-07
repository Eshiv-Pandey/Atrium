package http

import (
	"time"

	"github.com/google/uuid"

	"atrium/internal/domain"
	"atrium/internal/service"
	"atrium/internal/store"
)

// Wire types are defined separately from domain types on purpose.
//
// If handlers serialised domain structs directly, every field added to a
// domain type would silently appear in the API — including
// User.PasswordHash. Making the boundary explicit means exposing a field is
// always a deliberate edit to this file.
//
// All timestamps serialise as RFC 3339 with an offset, never a naive local
// time. The frontend renders them in the viewer's zone.

type userResponse struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  string    `json:"role"`
}

func newUserResponse(u *domain.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role)}
}

type roomResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Capacity  int       `json:"capacity"`
	Amenities []string  `json:"amenities"`

	// Available is omitted unless the caller supplied a time window, so the
	// client can tell "not asked" from "asked, and it is taken".
	Available *bool `json:"available,omitempty"`
}

func newRoomResponse(r domain.Room, available *bool) roomResponse {
	// A nil slice marshals to null; the client expects a list it can map over.
	amenities := r.Amenities
	if amenities == nil {
		amenities = []string{}
	}
	return roomResponse{
		ID:        r.ID,
		Name:      r.Name,
		Capacity:  r.Capacity,
		Amenities: amenities,
		Available: available,
	}
}

type bookingResponse struct {
	ID            uuid.UUID `json:"id"`
	RoomID        uuid.UUID `json:"roomId"`
	UserID        uuid.UUID `json:"userId"`
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime"`
	AttendeeCount int       `json:"attendeeCount"`
	Status        string    `json:"status"`
	CheckedInAt   *time.Time `json:"checkedInAt"`
	CreatedAt     time.Time  `json:"createdAt"`

	// CanCheckInNow saves every client from reimplementing the window
	// arithmetic — and from disagreeing with the server about it when their
	// clock is off. The server owns the rule; the client renders the answer.
	CanCheckInNow bool `json:"canCheckInNow"`

	// CanCancel likewise: a booking that has already started cannot be
	// cancelled, and the button should reflect that without a round trip.
	CanCancel bool `json:"canCancel"`

	// Room and User are present on list responses and absent on single-booking
	// ones. A list of bookings is unreadable without a room name, and the
	// admin table needs to say who made each booking; but the responses to
	// create and check-in go back to a caller who already has that context on
	// screen, and re-deriving it there would mean a join on the write path to
	// restate what the client just sent.
	//
	// User is populated only for admins. A member listing their own bookings
	// learns nothing from a copy of their own name, and the same DTO serving
	// both audiences is how one endpoint accidentally leaks the directory.
	Room *bookingRoomRef `json:"room,omitempty"`
	User *bookingUserRef `json:"user,omitempty"`
}

type bookingRoomRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type bookingUserRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

func newBookingResponse(b domain.Booking, now time.Time) bookingResponse {
	return bookingResponse{
		ID:            b.ID,
		RoomID:        b.RoomID,
		UserID:        b.UserID,
		StartTime:     b.StartTime,
		EndTime:       b.EndTime,
		AttendeeCount: b.AttendeeCount,
		Status:        string(b.Status),
		CheckedInAt:   b.CheckedInAt,
		CreatedAt:     b.CreatedAt,
		CanCheckInNow: canCheckInNow(b, now),
		CanCancel:     b.Status == domain.BookingStatusConfirmed && b.StartTime.After(now),
	}
}

// newBookingRowResponse is the list form: the same booking plus the names that
// make a row readable.
//
// includeUser gates the member's name and email on the caller being an admin.
func newBookingRowResponse(r store.BookingRow, now time.Time, includeUser bool) bookingResponse {
	out := newBookingResponse(r.Booking, now)
	out.Room = &bookingRoomRef{ID: r.RoomID, Name: r.RoomName}
	if includeUser {
		out.User = &bookingUserRef{ID: r.UserID, Name: r.UserName, Email: r.UserEmail}
	}
	return out
}

type intervalResponse struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func newIntervalResponses(ivs []service.Interval) []intervalResponse {
	out := make([]intervalResponse, len(ivs))
	for i, iv := range ivs {
		out[i] = intervalResponse{Start: iv.Start, End: iv.End}
	}
	return out
}

type availabilityResponse struct {
	Room  roomResponse       `json:"room"`
	Start time.Time          `json:"start"`
	End   time.Time          `json:"end"`
	Busy  []intervalResponse `json:"busy"`
	Free  []intervalResponse `json:"free"`
}

type utilizationResponse struct {
	RoomID   uuid.UUID `json:"roomId"`
	RoomName string    `json:"roomName"`
	Capacity int       `json:"capacity"`

	BookedHours  float64 `json:"bookedHours"`
	BookingCount int     `json:"bookingCount"`
	NoShowCount  int     `json:"noShowCount"`

	// UtilizationRate is booked hours over bookable hours in the window,
	// computed server-side so every consumer reports the same number.
	UtilizationRate float64 `json:"utilizationRate"`
}

func newUtilizationResponse(u store.RoomUtilization, windowHours float64) utilizationResponse {
	rate := 0.0
	if windowHours > 0 {
		rate = u.BookedHours / windowHours
	}
	return utilizationResponse{
		RoomID:          u.RoomID,
		RoomName:        u.RoomName,
		Capacity:        u.Capacity,
		BookedHours:     round2(u.BookedHours),
		BookingCount:    u.BookingCount,
		NoShowCount:     u.NoShowCount,
		UtilizationRate: round2(rate),
	}
}

// listResponse is the envelope for every collection endpoint.
//
// A bare JSON array leaves nowhere to put pagination state without a breaking
// change, so collections are always wrapped.
type listResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func newListResponse[T any](items []T, nextCursor *string) listResponse[T] {
	if items == nil {
		items = []T{}
	}
	return listResponse[T]{Items: items, NextCursor: nextCursor}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
