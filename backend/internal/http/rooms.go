package http

import (
	"net/http"

	"atrium/internal/service"
)

// RoomsHandler owns the rooms endpoints.
type RoomsHandler struct {
	avail *service.AvailabilityService
}

func NewRoomsHandler(avail *service.AvailabilityService) *RoomsHandler {
	return &RoomsHandler{avail: avail}
}

// ListRooms returns the room catalog, annotated with availability when a time
// window is supplied.
//
// GET /api/rooms?min_capacity=&amenities=&start=&end=
//
// The browse page uses this in two ways: without time params it gets the whole
// catalog for the left rail, and with them it asks "show me 4-person rooms
// with a TV that are free 2–4pm today" in one request. Separate endpoints for
// the two would make the client track two models and keep them in sync.
func (h *RoomsHandler) ListRooms(w http.ResponseWriter, r *http.Request) {
	p := newQueryParams(r)

	in := service.ListRoomsInput{
		MinCapacity: p.intVal("min_capacity", 0),
		Amenities:   p.csv("amenities"),
		Window:      p.window(),
	}

	if !p.ok() {
		writeValidationError(w, "One or more query parameters are invalid.", p.fields())
		return
	}

	rooms, err := h.avail.ListRooms(r.Context(), in)
	if err != nil {
		writeError(w, r, err)
		return
	}

	out := make([]roomResponse, len(rooms))
	for i, ra := range rooms {
		out[i] = newRoomResponse(ra.Room, ra.Available)
	}

	// Wrapped in the same envelope as every other collection, with a null
	// cursor. The catalog is small and deliberately unpaginated — a co-working
	// space has tens of rooms, not thousands — but a client that has to
	// remember which collections are bare arrays and which are envelopes will
	// eventually get it wrong. One shape for all of them.
	writeJSON(w, http.StatusOK, newListResponse(out, nil))
}

// GetDayAvailability returns one room's busy/free spans over a window.
//
// GET /api/rooms/{id}/availability?start=&end=
//
// The room-detail page uses this for the timeline: 9am–6pm shows as a
// horizontal bar, with busy slots shaded and free slots clickable. Both
// bounds required: a half-supplied window is a client bug that would silently
// read as "no window" and mislead.
func (h *RoomsHandler) GetDayAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}

	p := newQueryParams(r)
	window := p.requiredWindow()

	if !p.ok() {
		writeValidationError(w, "One or more query parameters are invalid.", p.fields())
		return
	}

	avail, err := h.avail.GetDayAvailability(r.Context(), id, window)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, availabilityResponse{
		Room:  newRoomResponse(avail.Room, nil),
		Start: avail.Start,
		End:   avail.End,
		Busy:  newIntervalResponses(avail.Busy),
		Free:  newIntervalResponses(avail.Free),
	})
}
