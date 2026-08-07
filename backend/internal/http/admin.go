package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"atrium/internal/domain"
	"atrium/internal/service"
	"atrium/internal/store"
)

// AdminHandler owns the admin-only endpoints. Every route it serves is mounted
// behind RequireAdmin; none of these methods re-check the role, because a
// second check in a second place is a second thing that can be forgotten.
type AdminHandler struct {
	admin    *service.AdminService
	bookings *service.BookingService
}

func NewAdminHandler(admin *service.AdminService, bookings *service.BookingService) *AdminHandler {
	return &AdminHandler{admin: admin, bookings: bookings}
}

type createRoomRequest struct {
	Name      string   `json:"name"`
	Capacity  int      `json:"capacity"`
	Amenities []string `json:"amenities"`
}

// CreateRoom adds a room to the catalog.
//
// POST /api/rooms
func (h *AdminHandler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid request body", domain.ErrValidation))
		return
	}

	name := strings.TrimSpace(req.Name)
	fields := map[string]string{}
	if name == "" {
		fields["name"] = "is required"
	}
	if req.Capacity < 1 {
		fields["capacity"] = "must be at least 1"
	}
	if len(fields) > 0 {
		writeValidationError(w, "One or more fields are invalid.", fields)
		return
	}

	room, err := h.admin.CreateRoom(r.Context(), &domain.Room{
		Name:      name,
		Capacity:  req.Capacity,
		Amenities: normalizeAmenities(req.Amenities),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, newRoomResponse(*room, nil))
}

// updateRoomRequest uses pointers so an omitted field is distinguishable from
// one explicitly set to its zero value.
//
// This is why the endpoint is PATCH rather than PUT: with a value type,
// {"name": "Board Room"} would silently read capacity as 0 and wipe it. The
// pointer makes "not mentioned" and "set to zero" different requests, and the
// latter is then correctly rejected by validation.
type updateRoomRequest struct {
	Name      *string   `json:"name"`
	Capacity  *int      `json:"capacity"`
	Amenities *[]string `json:"amenities"`
}

// UpdateRoom applies a partial update.
//
// PATCH /api/rooms/{id}
func (h *AdminHandler) UpdateRoom(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}

	var req updateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid request body", domain.ErrValidation))
		return
	}

	update := store.RoomUpdate{Capacity: req.Capacity, Amenities: req.Amenities}
	fields := map[string]string{}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			fields["name"] = "cannot be empty"
		}
		update.Name = &name
	}
	if req.Capacity != nil && *req.Capacity < 1 {
		fields["capacity"] = "must be at least 1"
	}
	if req.Amenities != nil {
		normalized := normalizeAmenities(*req.Amenities)
		update.Amenities = &normalized
	}

	if len(fields) > 0 {
		writeValidationError(w, "One or more fields are invalid.", fields)
		return
	}

	// Reducing capacity below an existing booking's attendee count is allowed
	// deliberately: the alternative is an admin who cannot correct a data entry
	// error because someone booked against the wrong number. Existing bookings
	// keep their slot; the constraint applies to new ones.
	room, err := h.admin.UpdateRoom(r.Context(), id, update)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newRoomResponse(*room, nil))
}

// DeleteRoom removes a room, refusing if future bookings depend on it.
//
// DELETE /api/rooms/{id}
func (h *AdminHandler) DeleteRoom(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.admin.DeleteRoom(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}

	writeNoContent(w)
}

// ListBookings returns bookings across all users.
//
// GET /api/bookings?room_id=&user_id=&from=&to=&status=&limit=&cursor=
func (h *AdminHandler) ListBookings(w http.ResponseWriter, r *http.Request) {
	p := newQueryParams(r)

	filter := bookingFilterFrom(p)
	filter.UserID = p.uuidVal("user_id")

	if !p.ok() {
		writeValidationError(w, "One or more query parameters are invalid.", p.fields())
		return
	}

	bookings, hasMore, err := h.bookings.List(r.Context(), filter)
	if err != nil {
		writeError(w, r, err)
		return
	}

	// includeUser: this is the admin table, and "who booked it" is the column
	// that makes it useful for chasing down a room nobody is using.
	writeJSON(w, http.StatusOK, bookingListResponse(bookings, hasMore, true))
}

// Utilization reports booked hours per room over a window.
//
// GET /api/admin/utilization?start=&end=
func (h *AdminHandler) Utilization(w http.ResponseWriter, r *http.Request) {
	p := newQueryParams(r)
	window := p.requiredWindow()

	if !p.ok() {
		writeValidationError(w, "One or more query parameters are invalid.", p.fields())
		return
	}

	rows, err := h.admin.Utilization(r.Context(), window)
	if err != nil {
		writeError(w, r, err)
		return
	}

	// The denominator for the rate is the whole window, not a business-hours
	// subset. Rooms are bookable around the clock in this model, so anything
	// narrower would report a utilization the booking rules do not support.
	windowHours := window.Duration().Hours()

	out := make([]utilizationResponse, len(rows))
	for i, u := range rows {
		out[i] = newUtilizationResponse(u, windowHours)
	}
	writeJSON(w, http.StatusOK, newListResponse(out, nil))
}

// normalizeAmenities trims, drops blanks, lowercases, and de-duplicates.
//
// Amenities are matched with the @> containment operator, which is exact: a
// room stored with "TV " would never match a filter for "tv". Normalising on
// the way in means the comparison can stay a simple indexed containment check
// instead of something case-folding and unindexable.
func normalizeAmenities(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, a := range in {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}
