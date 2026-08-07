package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"atrium/internal/domain"
	"atrium/internal/service"
	"atrium/internal/store"
)

// BookingsHandler owns the booking endpoints.
type BookingsHandler struct {
	bookings *service.BookingService
}

func NewBookingsHandler(bookings *service.BookingService) *BookingsHandler {
	return &BookingsHandler{bookings: bookings}
}

type createBookingRequest struct {
	RoomID        string `json:"roomId"`
	StartTime     string `json:"startTime"`
	EndTime       string `json:"endTime"`
	AttendeeCount int    `json:"attendeeCount"`
}

// Create reserves a room.
//
// POST /api/bookings
//
// Honours an optional Idempotency-Key header. A booking form that is
// double-clicked, or a request the client retried because the response was
// lost, must not produce two reservations — so a replayed key returns the
// original booking with 200 rather than creating a second one with 201.
func (h *BookingsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createBookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid request body", domain.ErrValidation))
		return
	}

	fields := map[string]string{}

	roomID, err := uuid.Parse(strings.TrimSpace(req.RoomID))
	if err != nil {
		fields["roomId"] = "must be a room UUID"
	}
	start, err := parseRFC3339(req.StartTime)
	if err != nil {
		fields["startTime"] = "must be an RFC 3339 timestamp with an offset"
	}
	end, err := parseRFC3339(req.EndTime)
	if err != nil {
		fields["endTime"] = "must be an RFC 3339 timestamp with an offset"
	}
	if req.AttendeeCount < 1 {
		fields["attendeeCount"] = "must be at least 1"
	}

	if len(fields) > 0 {
		writeValidationError(w, "One or more fields are invalid.", fields)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	identity := mustIdentity(r.Context())
	booking, replayed, err := h.bookings.Create(r.Context(), service.CreateBookingInput{
		RoomID:         roomID,
		UserID:         identity.UserID,
		Start:          start,
		End:            end,
		AttendeeCount:  req.AttendeeCount,
		IdempotencyKey: key,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	// 201 for a new reservation, 200 for a replay. A 201 for something that
	// was not created would be a lie the caller has no way to detect, and the
	// client uses the difference to decide between "Booked" and "You already
	// booked this".
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}

	writeJSON(w, status, newBookingResponse(*booking, time.Now()))
}

// ListMine returns the caller's own bookings.
//
// GET /api/bookings/me?from=&to=&status=&limit=&cursor=
func (h *BookingsHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	p := newQueryParams(r)
	filter := bookingFilterFrom(p)

	if !p.ok() {
		writeValidationError(w, "One or more query parameters are invalid.", p.fields())
		return
	}

	identity := mustIdentity(r.Context())
	bookings, hasMore, err := h.bookings.ListForUser(r.Context(), identity.UserID, filter)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, bookingListResponse(bookings, hasMore, false))
}

// CheckIn records that the member turned up.
//
// POST /api/bookings/{id}/check-in
func (h *BookingsHandler) CheckIn(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}

	identity := mustIdentity(r.Context())
	booking, err := h.bookings.CheckIn(r.Context(), id, identity.UserID, identity.IsAdmin())
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newBookingResponse(*booking, time.Now()))
}

// Cancel releases a booking the caller owns.
//
// DELETE /api/bookings/{id}
//
// 204 on success and 204 again on a booking that was already cancelled: the
// caller's desired state holds either way, and a client retrying after a
// dropped response should not see an error for work that succeeded.
func (h *BookingsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}

	identity := mustIdentity(r.Context())
	if err := h.bookings.Cancel(r.Context(), id, identity.UserID, identity.IsAdmin()); err != nil {
		writeError(w, r, err)
		return
	}

	writeNoContent(w)
}

// bookingFilterFrom reads the filter shared by the member and admin listings,
// so the two endpoints cannot drift in how they interpret the same parameters.
func bookingFilterFrom(p *queryParams) store.BookingFilter {
	cursorStart, cursorID := p.cursor()
	return store.BookingFilter{
		RoomID:      p.uuidVal("room_id"),
		From:        p.timeVal("from"),
		To:          p.timeVal("to"),
		Status:      p.statusVal("status"),
		Limit:       p.intVal("limit", 0),
		CursorStart: cursorStart,
		CursorID:    cursorID,
	}
}

// bookingListResponse wraps a page of bookings and computes the next cursor.
//
// includeUser is passed through to the DTO: admins see who made each booking,
// members do not.
func bookingListResponse(bookings []store.BookingRow, hasMore bool, includeUser bool) listResponse[bookingResponse] {
	now := time.Now()

	items := make([]bookingResponse, len(bookings))
	for i, b := range bookings {
		items[i] = newBookingRowResponse(b, now, includeUser)
	}

	var next *string
	if hasMore && len(bookings) > 0 {
		last := bookings[len(bookings)-1]
		c := encodeCursor(last.StartTime, last.ID)
		next = &c
	}
	return newListResponse(items, next)
}

// maxIdempotencyKeyLength bounds what a client may send, since the value is
// stored and indexed. 255 is generous for a UUID or a hash and small enough
// that the unique index stays cheap.
const maxIdempotencyKeyLength = 255

// idempotencyKey reads and validates the Idempotency-Key header.
func idempotencyKey(r *http.Request) (*string, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return nil, nil
	}
	if len(key) > maxIdempotencyKeyLength {
		return nil, fmt.Errorf("%w: Idempotency-Key may be at most %d characters",
			domain.ErrValidation, maxIdempotencyKeyLength)
	}
	return &key, nil
}

// parseRFC3339 accepts only timestamps carrying an offset.
//
// time.Parse with the RFC3339 layout already rejects a missing offset, so this
// is mostly a named home for the rule and its reason: the wire format never
// carries a naive local time, in either direction.
func parseRFC3339(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	return time.Parse(time.RFC3339, s)
}
