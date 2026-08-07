package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"atrium/internal/domain"
)

// paramsFor takes an already-encoded query string, for the tables that are
// about query-string spelling itself: repeated keys, commas, padding.
func paramsFor(t *testing.T, query string) *queryParams {
	t.Helper()
	return newQueryParams(httptest.NewRequest(http.MethodGet, "/api/bookings?"+query, nil))
}

// paramsWith encodes one key/value properly, for the tables whose values are
// hostile to a query string.
//
// Concatenating these by hand is a trap worth naming: a literal '+' in
// "09:00:00+05:30" decodes to a space, so the handler under test would receive
// "09:00:00 05:30" and reject it — and the test would look like it had caught a
// parser bug rather than an encoding mistake of its own. A raw space is worse
// still: it breaks the request line and panics before any assertion runs.
func paramsWith(t *testing.T, key, value string) *queryParams {
	t.Helper()
	return paramsFor(t, url.Values{key: {value}}.Encode())
}

// TestTimeVal_RejectsNaiveTimestamps is the timezone rule, enforced at the edge.
//
// A booking system that accepts "2026-01-02T09:00" has to guess a zone, and
// guessing wrong by an hour is the most common booking bug there is. The client
// knows the viewer's zone and converts before asking, so the server never has to
// guess — but only if this rejects the naive form rather than helpfully assuming
// UTC.
func TestTimeVal_RejectsNaiveTimestamps(t *testing.T) {
	accepted := []struct {
		name, raw string
		wantUTC   string
	}{
		{"UTC with Z", "2026-01-02T09:00:00Z", "2026-01-02T09:00:00Z"},
		{"positive offset", "2026-01-02T09:00:00+05:30", "2026-01-02T03:30:00Z"},
		{"negative offset", "2026-01-02T09:00:00-05:00", "2026-01-02T14:00:00Z"},
		{"fractional seconds", "2026-01-02T09:00:00.123Z", "2026-01-02T09:00:00.123Z"},
	}

	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			p := paramsWith(t, "start", tc.raw)
			got := p.timeVal("start")
			if got == nil {
				t.Fatalf("rejected %q: %v", tc.raw, p.fields())
			}
			if !p.ok() {
				t.Errorf("recorded a field error for a valid timestamp: %v", p.fields())
			}
			// The offset must be honoured, not discarded. A parser that read the
			// digits and ignored the zone would pass a presence check and be
			// wrong by hours.
			want, err := time.Parse(time.RFC3339Nano, tc.wantUTC)
			if err != nil {
				t.Fatalf("bad test expectation: %v", err)
			}
			if !got.Equal(want) {
				t.Errorf("%q parsed to %s, want %s", tc.raw, got.UTC(), want)
			}
		})
	}

	rejected := []struct{ name, raw string }{
		{"no zone", "2026-01-02T09:00:00"},
		{"date only", "2026-01-02"},
		{"no seconds", "2026-01-02T09:00Z"},
		{"a date in words", "2 January 2026"},
		{"unix seconds", "1767344400"},
		{"nonsense", "tomorrow-ish"},
		// What a naive client produces by pasting a timestamp into a URL without
		// escaping it: the '+' arrives as a space. Rejecting it is right — the
		// alternative is guessing which of the two the caller meant.
		{"offset mangled by an unescaped plus", "2026-01-02T09:00:00 05:30"},
		{"empty-ish whitespace", "   "},
	}

	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			p := paramsWith(t, "start", tc.raw)
			if got := p.timeVal("start"); got != nil {
				t.Errorf("accepted %q as %s", tc.raw, got)
			}
			// Whitespace-only trims to empty, which is "absent" rather than
			// "malformed" — absent is not an error, so this row asserts only
			// that nothing was parsed.
			if tc.name == "empty-ish whitespace" {
				return
			}
			if p.ok() {
				t.Errorf("no field error recorded for %q", tc.raw)
			}
			if msg := p.fields()["start"]; !containsFold(msg, "RFC 3339") {
				t.Errorf("message %q does not tell the client what format to use", msg)
			}
		})
	}
}

// TestQueryParams_AccumulatesEveryFieldError checks that a request with several
// bad parameters reports all of them.
//
// Failing on the first would make a client fix three mistakes in three round
// trips, and a form would highlight one field at a time. Same reasoning as
// validating a whole form at once.
func TestQueryParams_AccumulatesEveryFieldError(t *testing.T) {
	p := paramsFor(t, "start=not-a-time&min_capacity=lots&room_id=nope&status=maybe")

	p.timeVal("start")
	p.intVal("min_capacity", 0)
	p.uuidVal("room_id")
	p.statusVal("status")

	if p.ok() {
		t.Fatal("four malformed parameters produced no errors")
	}
	for _, key := range []string{"start", "min_capacity", "room_id", "status"} {
		if _, found := p.fields()[key]; !found {
			t.Errorf("no error reported for %q; got %v", key, p.fields())
		}
	}
	if got := len(p.fields()); got != 4 {
		t.Errorf("reported %d field errors, want 4: %v", got, p.fields())
	}
}

func TestIntVal(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		def     int
		want    int
		wantErr bool
	}{
		{"absent uses the default", "", 5, 5, false},
		{"present", "min_capacity=12", 5, 12, false},
		{"zero is a value, not absence", "min_capacity=0", 5, 0, false},
		// Negative parses fine here; the service layer decides whether it is
		// meaningful. Parsing and validating are separate jobs, and conflating
		// them here would put capacity rules in the transport layer.
		{"negative parses", "min_capacity=-3", 5, -3, false},
		{"not a number", "min_capacity=twelve", 5, 5, true},
		{"float", "min_capacity=1.5", 5, 5, true},
		{"whitespace is trimmed", "min_capacity=%207%20", 5, 7, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := paramsFor(t, tc.query)
			if got := p.intVal("min_capacity", tc.def); got != tc.want {
				t.Errorf("intVal = %d, want %d", got, tc.want)
			}
			if p.ok() == tc.wantErr {
				t.Errorf("ok() = %v, want %v (fields: %v)", p.ok(), !tc.wantErr, p.fields())
			}
		})
	}
}

// TestCSV accepts both spellings of a list because both are common: Go's own
// url.Values encodes repeats, while most hand-written clients use commas.
// Rejecting either is a papercut with nothing gained.
func TestCSV(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"absent", "", nil},
		{"single", "amenities=tv", []string{"tv"}},
		{"comma separated", "amenities=tv,whiteboard", []string{"tv", "whiteboard"}},
		{"repeated parameter", "amenities=tv&amenities=whiteboard", []string{"tv", "whiteboard"}},
		{"mixed", "amenities=tv,whiteboard&amenities=videoconf", []string{"tv", "whiteboard", "videoconf"}},
		{"padded items are trimmed", "amenities=%20tv%20,%20whiteboard", []string{"tv", "whiteboard"}},
		// Trailing commas come from string-joining an array with an empty last
		// element, which happens in real clients. An empty amenity would match
		// nothing and filter the catalogue down to zero rooms.
		{"empty items are dropped", "amenities=tv,,whiteboard,", []string{"tv", "whiteboard"}},
		{"only separators yields nothing", "amenities=,,,", nil},
		{"present but empty yields nothing", "amenities=", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := paramsFor(t, tc.query).csv("amenities")
			if len(got) != len(tc.want) {
				t.Fatalf("csv = %q, want %q", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("item %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestStatusVal(t *testing.T) {
	for _, want := range []domain.BookingStatus{
		domain.BookingStatusConfirmed,
		domain.BookingStatusCancelled,
		domain.BookingStatusReleased,
	} {
		t.Run("accepts "+string(want), func(t *testing.T) {
			p := paramsFor(t, "status="+string(want))
			got := p.statusVal("status")
			if got == nil || *got != want {
				t.Fatalf("statusVal = %v, want %s (fields: %v)", got, want, p.fields())
			}
		})
	}

	// Case matters: the column has a CHECK constraint listing lowercase values,
	// so accepting "Confirmed" here would build a filter that silently matches
	// nothing rather than erroring.
	for _, raw := range []string{"pending", "CONFIRMED", "Cancelled", "deleted", "'; drop table bookings--"} {
		t.Run("rejects "+raw, func(t *testing.T) {
			// paramsWith, not paramsFor: these values contain spaces and quotes
			// that break the request line if concatenated raw.
			p := paramsWith(t, "status", raw)
			if got := p.statusVal("status"); got != nil {
				t.Errorf("accepted %q as %q", raw, *got)
			}
			if p.ok() {
				t.Error("no field error recorded")
			}
			// The message lists the valid values, so the client does not have to
			// read the source to find them.
			if msg := p.fields()["status"]; !containsFold(msg, "confirmed") {
				t.Errorf("message %q does not list the accepted values", msg)
			}
		})
	}

	t.Run("absent is not an error", func(t *testing.T) {
		p := paramsFor(t, "")
		if got := p.statusVal("status"); got != nil {
			t.Errorf("statusVal = %v for an absent parameter, want nil", got)
		}
		if !p.ok() {
			t.Errorf("absent parameter recorded an error: %v", p.fields())
		}
	})
}

// TestWindow_BothBoundsOrNeither covers the half-supplied window.
//
// Treating "?start=..." with no end as "no window" is the dangerous reading: the
// browse page would come back with every room marked available and the member
// would book into an occupied slot. Better to reject the request.
func TestWindow_BothBoundsOrNeither(t *testing.T) {
	const (
		start = "2026-01-02T09:00:00Z"
		end   = "2026-01-02T10:00:00Z"
	)

	t.Run("neither is no window", func(t *testing.T) {
		p := paramsFor(t, "")
		if w := p.window(); w != nil {
			t.Errorf("window = %v, want nil", w)
		}
		if !p.ok() {
			t.Errorf("absent window recorded an error: %v", p.fields())
		}
	})

	t.Run("both is a window", func(t *testing.T) {
		p := paramsFor(t, "start="+start+"&end="+end)
		w := p.window()
		if w == nil {
			t.Fatalf("window = nil, want an interval (fields: %v)", p.fields())
		}
		if w.Duration() != time.Hour {
			t.Errorf("window spans %s, want 1h", w.Duration())
		}
	})

	t.Run("start alone is an error on end", func(t *testing.T) {
		p := paramsFor(t, "start="+start)
		if w := p.window(); w != nil {
			t.Errorf("window = %v, want nil", w)
		}
		if _, found := p.fields()["end"]; !found {
			t.Errorf("no error on the missing bound; got %v", p.fields())
		}
	})

	t.Run("end alone is an error on start", func(t *testing.T) {
		p := paramsFor(t, "end="+end)
		if w := p.window(); w != nil {
			t.Errorf("window = %v, want nil", w)
		}
		if _, found := p.fields()["start"]; !found {
			t.Errorf("no error on the missing bound; got %v", p.fields())
		}
	})

	// An inverted window parses: both bounds are well-formed RFC 3339. Whether
	// end precedes start is a rule, and rules live in the service layer, which
	// validateWindow enforces. This documents the division rather than
	// duplicating the check here.
	t.Run("inverted parses and is left to the service layer", func(t *testing.T) {
		p := paramsFor(t, "start="+end+"&end="+start)
		w := p.window()
		if w == nil {
			t.Fatalf("window = nil; parsing should succeed (fields: %v)", p.fields())
		}
		if !w.IsEmpty() {
			t.Error("an inverted window should report IsEmpty, which is what validateWindow rejects")
		}
	})
}

func TestRequiredWindow_NamesBothMissingBounds(t *testing.T) {
	p := paramsFor(t, "")
	p.requiredWindow()

	if p.ok() {
		t.Fatal("a required window with neither bound produced no error")
	}
	for _, key := range []string{"start", "end"} {
		if _, found := p.fields()[key]; !found {
			t.Errorf("no error reported for %q; got %v", key, p.fields())
		}
	}
}

// TestCursorRoundTrip is the pagination key's contract.
//
// The cursor carries (start_time, id) and the query compares it as a tuple, so a
// cursor that loses precision skips or repeats rows — a bug that shows up as
// "one booking missing from page two" and is nearly impossible to reproduce
// from a report. Nanosecond precision is the part most easily lost, hence the
// awkward timestamps below.
func TestCursorRoundTrip(t *testing.T) {
	id := uuid.New()

	cases := []struct {
		name  string
		start time.Time
	}{
		{"whole second UTC", time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)},
		{"nanosecond precision", time.Date(2026, 6, 15, 9, 0, 0, 123456789, time.UTC)},
		{"a single nanosecond", time.Date(2026, 6, 15, 9, 0, 0, 1, time.UTC)},
		// Encoded in UTC, so a non-UTC input must come back as the same instant.
		// Comparing with Equal rather than == is the point: == would compare the
		// location too and fail for the same moment in time.
		{"offset zone", time.Date(2026, 6, 15, 9, 0, 0, 0, time.FixedZone("IST", 5*3600+1800))},
		{"pre-epoch", time.Date(1969, 7, 20, 20, 17, 40, 0, time.UTC)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotID, err := decodeCursor(encodeCursor(tc.start, id))
			if err != nil {
				t.Fatalf("decode a cursor we just encoded: %v", err)
			}
			if !gotStart.Equal(tc.start) {
				t.Errorf("start = %s, want %s", gotStart.UTC(), tc.start.UTC())
			}
			if *gotID != id {
				t.Errorf("id = %s, want %s", gotID, id)
			}
		})
	}
}

// TestCursor_MalformedIsAFieldErrorNotACrash covers the cursor a client made up.
//
// The cursor is opaque, so a client cannot construct a valid one — but it can
// truncate, corrupt, or invent one, and every branch of decodeCursor has to
// survive that. The message says how to recover, because a stuck client that
// cannot page is worse than one that starts over.
func TestCursor_MalformedIsAFieldErrorNotACrash(t *testing.T) {
	valid := encodeCursor(time.Now(), uuid.New())

	for _, tc := range []struct{ name, raw string }{
		{"not base64", "!!!not-base64!!!"},
		{"base64 of nothing useful", "YWJjZGVm"},                       // "abcdef"
		{"no separator", "MjAyNi0wNi0xNVQwOTowMDowMFo"},                // a timestamp alone
		{"bad timestamp", "bm90LWEtdGltZXwxMjM"},                       // "not-a-time|123"
		{"bad uuid", "MjAyNi0wNi0xNVQwOTowMDowMFp8bm90LWEtdXVpZA"},      // valid time, bad id
		{"truncated valid cursor", valid[:len(valid)/2]},
		{"empty separator halves", "fA"}, // "|"
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := paramsFor(t, "cursor="+tc.raw)
			start, id := p.cursor()

			if start != nil || id != nil {
				t.Errorf("decoded %q to (%v, %v), want nothing", tc.raw, start, id)
			}
			if p.ok() {
				t.Error("no field error recorded for a malformed cursor")
			}
			if msg := p.fields()["cursor"]; !containsFold(msg, "omit it") {
				t.Errorf("message %q does not tell the client how to recover", msg)
			}
		})
	}

	t.Run("absent is not an error", func(t *testing.T) {
		p := paramsFor(t, "")
		if start, id := p.cursor(); start != nil || id != nil {
			t.Errorf("absent cursor decoded to (%v, %v)", start, id)
		}
		if !p.ok() {
			t.Errorf("absent cursor recorded an error: %v", p.fields())
		}
	})
}

// TestEncodeCursor_IsURLSafe matters because the cursor goes back in a query
// string. Standard base64 uses '+' and '/', which would need escaping and
// arrive corrupted from any client that forgets.
func TestEncodeCursor_IsURLSafe(t *testing.T) {
	// Enough samples to hit the byte patterns that standard base64 would encode
	// with '+' or '/'; the alphabet is what is under test, not any one value.
	for i := 0; i < 200; i++ {
		got := encodeCursor(time.Now().Add(time.Duration(i)*time.Nanosecond), uuid.New())
		for _, c := range got {
			isSafe := c == '-' || c == '_' ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !isSafe {
				t.Fatalf("cursor %q contains %q, which is not URL-safe", got, c)
			}
		}
	}
}
