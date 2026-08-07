package http

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"atrium/internal/domain"
	"atrium/internal/service"
)

// queryParams parses query string values, accumulating a problem per field.
//
// Parsing continues past the first bad value so a request with three malformed
// parameters reports all three at once, instead of making the caller fix them
// one round trip at a time. Same reasoning as config.Load, applied to requests.
type queryParams struct {
	values url.Values
	errs   map[string]string
}

func newQueryParams(r *http.Request) *queryParams {
	return &queryParams{values: r.URL.Query(), errs: map[string]string{}}
}

func (p *queryParams) fail(key, msg string) { p.errs[key] = msg }

// ok reports whether every parameter read so far was valid.
func (p *queryParams) ok() bool { return len(p.errs) == 0 }

func (p *queryParams) fields() map[string]string { return p.errs }

// str returns a trimmed value, or "" when the parameter is absent.
func (p *queryParams) str(key string) string {
	return strings.TrimSpace(p.values.Get(key))
}

// intVal returns an integer parameter, or def when absent.
func (p *queryParams) intVal(key string, def int) int {
	raw := p.str(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		p.fail(key, "must be a whole number")
		return def
	}
	return n
}

// timeVal returns an instant, or nil when absent.
//
// RFC 3339 with an offset is the only accepted form. A naive
// "2026-01-02T09:00" would force the server to guess a zone, and guessing
// wrong by one hour is the most common class of booking bug there is. The
// client knows the viewer's zone; it converts before asking.
func (p *queryParams) timeVal(key string) *time.Time {
	raw := p.str(key)
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		p.fail(key, "must be an RFC 3339 timestamp with an offset, e.g. 2026-01-02T09:00:00Z")
		return nil
	}
	return &t
}

// uuidVal returns a UUID parameter, or nil when absent.
func (p *queryParams) uuidVal(key string) *uuid.UUID {
	raw := p.str(key)
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		p.fail(key, "must be a UUID")
		return nil
	}
	return &id
}

// csv returns a comma-separated list, with blanks dropped.
//
// Repeated parameters (?amenities=tv&amenities=whiteboard) are accepted too,
// because both spellings are common enough that rejecting one is a papercut.
func (p *queryParams) csv(key string) []string {
	raw := p.values[key]
	if len(raw) == 0 {
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, group := range raw {
		for _, item := range strings.Split(group, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// statusVal returns a booking status filter, or nil when absent.
func (p *queryParams) statusVal(key string) *domain.BookingStatus {
	raw := p.str(key)
	if raw == "" {
		return nil
	}

	s := domain.BookingStatus(raw)
	switch s {
	case domain.BookingStatusConfirmed, domain.BookingStatusCancelled, domain.BookingStatusReleased:
		return &s
	}
	p.fail(key, "must be one of: confirmed, cancelled, released")
	return nil
}

// window returns the [start, end) interval a request asked about.
//
// Both bounds or neither: a half-supplied window is a client bug that would
// otherwise be silently interpreted as "no window", and the caller would
// wonder why every room came back available.
func (p *queryParams) window() *service.Interval {
	start, end := p.timeVal("start"), p.timeVal("end")

	switch {
	case start == nil && end == nil:
		return nil
	case start == nil:
		p.fail("start", "required when end is given")
		return nil
	case end == nil:
		p.fail("end", "required when start is given")
		return nil
	}
	return &service.Interval{Start: *start, End: *end}
}

// requiredWindow is window() for endpoints where the range is not optional.
func (p *queryParams) requiredWindow() service.Interval {
	w := p.window()
	if w == nil {
		if p.str("start") == "" {
			p.fail("start", "required")
		}
		if p.str("end") == "" {
			p.fail("end", "required")
		}
		return service.Interval{}
	}
	return *w
}

// cursor decodes the pagination cursor into its (start_time, id) parts.
func (p *queryParams) cursor() (*time.Time, *uuid.UUID) {
	raw := p.str("cursor")
	if raw == "" {
		return nil, nil
	}

	start, id, err := decodeCursor(raw)
	if err != nil {
		p.fail("cursor", "is not a valid cursor; omit it to start from the first page")
		return nil, nil
	}
	return start, id
}

// encodeCursor packs the sort key of a page's last row.
//
// Deliberately opaque: it encodes the internal (start_time, id) ordering key,
// and keeping it base64 means the pagination key can change later without
// breaking clients that stored one. Clients are expected to echo it back, not
// to construct one.
func encodeCursor(start time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(start.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(s string) (*time.Time, *uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, nil, err
	}

	rawStart, rawID, found := strings.Cut(string(decoded), "|")
	if !found {
		return nil, nil, errMalformedCursor
	}

	start, err := time.Parse(time.RFC3339Nano, rawStart)
	if err != nil {
		return nil, nil, err
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, nil, err
	}
	return &start, &id, nil
}

var errMalformedCursor = errors.New("malformed cursor")

// pathUUID reads a UUID from a URL path segment.
//
// A malformed id is 404 rather than 422: the caller asked for a resource at a
// path that cannot name one, and "no such thing" is both true and the same
// answer they would get for a well-formed id that does not exist. Returning
// 422 here would also let a caller distinguish "invalid" from "not yours",
// which is a small enumeration hint.
func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		return uuid.Nil, domain.ErrNotFound
	}
	return id, nil
}
