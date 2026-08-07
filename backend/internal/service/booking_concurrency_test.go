package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"atrium/internal/domain"
	"atrium/internal/service"
	"atrium/internal/store"
	"atrium/internal/testutil"
)

// newBookingService assembles the real service over a real database. Nothing is
// faked: the behaviour under test is enforced by a Postgres constraint, so a
// fake store would be testing the fake.
func newBookingService(t *testing.T) (*service.BookingService, *store.DB) {
	t.Helper()
	db := testutil.DB(t)
	return service.NewBookingService(db, store.NewBookingStore(db), store.NewRoomStore(db)), db
}

// slot returns a one-hour window starting `hoursFromNow` from now, aligned to
// the hour so the times read cleanly in failure output.
func slot(hoursFromNow int) (time.Time, time.Time) {
	start := time.Now().Add(time.Duration(hoursFromNow) * time.Hour).Truncate(time.Hour)
	// Truncate can round back past `now` for small offsets; push forward so the
	// service's past-booking rule is not what the test trips over.
	if !start.After(time.Now()) {
		start = start.Add(time.Hour)
	}
	return start, start.Add(time.Hour)
}

// TestCreate_ConcurrentSameSlot is the test this schema exists for.
//
// N members book the identical slot in the identical room at the identical
// moment. Exactly one must succeed. This is the assertion that distinguishes a
// real solution from the common broken one: check-then-insert passes every
// sequential test and fails here, because two requests can both read "free"
// before either writes.
//
// The goroutines are released from a barrier rather than merely started in a
// loop. Started in a loop, the first is usually committed before the last is
// spawned and the test passes against code that has no concurrency safety at
// all — a green result that proves nothing.
func TestCreate_ConcurrentSameSlot(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	room := testutil.Room(t, db, 10)
	start, end := slot(2)

	// Below the pool's MaxConns of 10, so every attempt holds a connection at
	// once. Above it, the excess would queue on the pool and be serialised
	// there — arriving one at a time, which is the situation this test exists
	// to rule out.
	const attempts = 8

	// Distinct users, because the realistic race is different people wanting
	// the same room. It also proves the constraint is scoped to the room and
	// not accidentally to the user: one user booking twice would additionally
	// trip the idempotency index, and a pass could then mean the wrong
	// constraint fired.
	users := make([]*domain.User, attempts)
	for i := range users {
		users[i] = testutil.User(t, db, domain.RoleMember)
	}

	var (
		release  = make(chan struct{})
		ready    sync.WaitGroup
		finished sync.WaitGroup

		mu        sync.Mutex
		succeeded []*domain.Booking
		conflicts int
		other     []error
	)

	ready.Add(attempts)
	finished.Add(attempts)

	for i := 0; i < attempts; i++ {
		go func(u *domain.User) {
			defer finished.Done()

			ready.Done()
			<-release // every goroutine unblocks on the same close

			b, replayed, err := svc.Create(ctx, service.CreateBookingInput{
				RoomID:        room.ID,
				UserID:        u.ID,
				Start:         start,
				End:           end,
				AttendeeCount: 1,
				// No idempotency key. With one, a replay would be indistinguishable
				// from a conflict in the counts below, and the constraint under
				// test here is the overlap one.
				IdempotencyKey: nil,
			})

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				if replayed {
					other = append(other, errors.New("reported a replay without an idempotency key"))
					return
				}
				succeeded = append(succeeded, b)
			case errors.Is(err, domain.ErrConflict):
				conflicts++
			default:
				// Anything else is a bug, not a lost race. Collected rather than
				// t.Error'd because t.Error from a goroutine races the test's own
				// completion.
				other = append(other, err)
			}
		}(users[i])
	}

	ready.Wait()
	close(release)
	finished.Wait()

	for _, err := range other {
		t.Errorf("unexpected error (want nil or ErrConflict): %v", err)
	}

	if len(succeeded) != 1 {
		t.Errorf("got %d successful bookings, want exactly 1", len(succeeded))
	}
	if conflicts != attempts-1 {
		t.Errorf("got %d conflicts, want %d", conflicts, attempts-1)
	}

	// The counts above describe what the service reported. This asserts what
	// the database actually holds — the claim being made is about stored state,
	// and a service that returned one success while writing two rows would
	// satisfy every assertion so far.
	var stored int
	err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM bookings
		 WHERE room_id = $1 AND status = 'confirmed'`, room.ID).Scan(&stored)
	if err != nil {
		t.Fatalf("count stored bookings: %v", err)
	}
	if stored != 1 {
		t.Errorf("database holds %d confirmed bookings for the slot, want 1", stored)
	}
}

// TestCreate_ConcurrentDistinctRoomsAllSucceed guards the key the room lock is
// taken on.
//
// Booking serialises per room by design, and the correctness of that does not
// depend on the key: a lock on a single global constant would serialise every
// booking in the building and still pass every other test in this file, only
// slower. What it would break is this — simultaneous bookings of *different*
// rooms, which must all succeed and must not contend at all.
//
// Deliberately no timing assertion: a wall-clock threshold is the flakiest kind
// of test on shared CI hardware. What is asserted instead is that every one
// succeeds, which a lock too coarse to be correct would still satisfy but which
// a lock that deadlocks or errors across rooms would not.
func TestCreate_ConcurrentDistinctRoomsAllSucceed(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	const attempts = 8
	start, end := slot(2)

	rooms := make([]*domain.Room, attempts)
	users := make([]*domain.User, attempts)
	for i := range rooms {
		rooms[i] = testutil.Room(t, db, 4)
		users[i] = testutil.User(t, db, domain.RoleMember)
	}

	var (
		release  = make(chan struct{})
		ready    sync.WaitGroup
		finished sync.WaitGroup

		mu   sync.Mutex
		errs []error
	)

	ready.Add(attempts)
	finished.Add(attempts)

	for i := 0; i < attempts; i++ {
		go func(room *domain.Room, u *domain.User) {
			defer finished.Done()
			ready.Done()
			<-release

			_, _, err := svc.Create(ctx, service.CreateBookingInput{
				RoomID: room.ID, UserID: u.ID, Start: start, End: end, AttendeeCount: 1,
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(rooms[i], users[i])
	}

	ready.Wait()
	close(release)
	finished.Wait()

	for _, err := range errs {
		t.Errorf("booking a distinct room concurrently failed: %v", err)
	}

	var stored int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE status = 'confirmed'`).Scan(&stored); err != nil {
		t.Fatalf("count stored bookings: %v", err)
	}
	if stored != attempts {
		t.Errorf("database holds %d confirmed bookings, want %d", stored, attempts)
	}
}

// TestCreate_BackToBackIsAllowed guards the '[)' bounds on the exclusion
// constraint.
//
// A meeting ending at 11:00 and one starting at 11:00 do not overlap. Naive
// overlap logic on closed ranges rejects them, which would make half the
// bookings in a working day impossible — the kind of bug that looks like
// correct strictness until someone tries to book the next hour.
func TestCreate_BackToBackIsAllowed(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	room := testutil.Room(t, db, 4)
	user := testutil.User(t, db, domain.RoleMember)
	start, mid := slot(2)
	end := mid.Add(time.Hour)

	if _, _, err := svc.Create(ctx, service.CreateBookingInput{
		RoomID: room.ID, UserID: user.ID, Start: start, End: mid, AttendeeCount: 1,
	}); err != nil {
		t.Fatalf("first booking: %v", err)
	}

	if _, _, err := svc.Create(ctx, service.CreateBookingInput{
		RoomID: room.ID, UserID: user.ID, Start: mid, End: end, AttendeeCount: 1,
	}); err != nil {
		t.Fatalf("back-to-back booking starting exactly when the first ends: %v", err)
	}
}

// TestCreate_OverlapVariants covers the ways two bookings can intersect.
//
// Table-driven because the interesting part is the boundary arithmetic, and
// one case per relationship makes an off-by-one visible as a single failing row
// rather than as a vague "overlap is broken".
func TestCreate_OverlapVariants(t *testing.T) {
	// A day out, not a few hours: several cases below start before `base`, and
	// from a nearer base those would land in the past and be refused by the
	// past-booking rule instead of being accepted. The row would still pass —
	// it wants no conflict and a validation error is not a conflict — while
	// testing nothing. Asserting success explicitly below closes the same gap
	// from the other side.
	base, baseEnd := slot(24)

	cases := []struct {
		name         string
		start, end   time.Time
		wantConflict bool
	}{
		{"identical", base, baseEnd, true},
		{"contained entirely within", base.Add(15 * time.Minute), baseEnd.Add(-15 * time.Minute), true},
		{"encloses the existing booking", base.Add(-time.Hour), baseEnd.Add(time.Hour), true},
		{"overlaps the start", base.Add(-30 * time.Minute), base.Add(30 * time.Minute), true},
		{"overlaps the end", baseEnd.Add(-30 * time.Minute), baseEnd.Add(30 * time.Minute), true},
		{"ends exactly at the start", base.Add(-time.Hour), base, false},
		{"starts exactly at the end", baseEnd, baseEnd.Add(time.Hour), false},
		{"entirely before", base.Add(-3 * time.Hour), base.Add(-2 * time.Hour), false},
		{"entirely after", baseEnd.Add(2 * time.Hour), baseEnd.Add(3 * time.Hour), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh database per case, so each one is independent: a leaked
			// booking from a previous row would make later rows conflict for
			// the wrong reason.
			svc, db := newBookingService(t)
			ctx := context.Background()

			room := testutil.Room(t, db, 4)
			user := testutil.User(t, db, domain.RoleMember)

			if _, _, err := svc.Create(ctx, service.CreateBookingInput{
				RoomID: room.ID, UserID: user.ID, Start: base, End: baseEnd, AttendeeCount: 1,
			}); err != nil {
				t.Fatalf("seed booking: %v", err)
			}

			_, _, err := svc.Create(ctx, service.CreateBookingInput{
				RoomID: room.ID, UserID: user.ID, Start: tc.start, End: tc.end, AttendeeCount: 1,
			})

			gotConflict := errors.Is(err, domain.ErrConflict)
			if gotConflict != tc.wantConflict {
				t.Errorf("conflict = %v (err: %v), want conflict = %v", gotConflict, err, tc.wantConflict)
			}
			// A non-overlapping case must actually succeed, not merely fail to
			// conflict. Without this, a validation error would satisfy the
			// assertion above and the row would report a passing test for a
			// booking that was never made.
			if !tc.wantConflict && err != nil {
				t.Errorf("non-overlapping booking was rejected: %v", err)
			}
		})
	}
}

// TestCreate_DifferentRoomsSameTime confirms the constraint is per-room.
//
// A constraint on the time range alone would pass every overlap test above and
// still be badly wrong: it would allow only one meeting in the entire building
// at a time.
func TestCreate_DifferentRoomsSameTime(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	roomA := testutil.Room(t, db, 4)
	roomB := testutil.Room(t, db, 4)
	user := testutil.User(t, db, domain.RoleMember)
	start, end := slot(2)

	for _, room := range []*domain.Room{roomA, roomB} {
		if _, _, err := svc.Create(ctx, service.CreateBookingInput{
			RoomID: room.ID, UserID: user.ID, Start: start, End: end, AttendeeCount: 1,
		}); err != nil {
			t.Fatalf("booking %s at the same time as the other room: %v", room.Name, err)
		}
	}
}

// TestCreate_CancelFreesTheSlot confirms a cancelled booking stops occupying
// its slot.
//
// This is the exclusion constraint's WHERE status = 'confirmed' predicate seen
// from the product side: cancel has to actually give the room back, or the
// schedule fills up with reservations nobody holds.
func TestCreate_CancelFreesTheSlot(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	room := testutil.Room(t, db, 4)
	first := testutil.User(t, db, domain.RoleMember)
	second := testutil.User(t, db, domain.RoleMember)
	start, end := slot(4)

	booking, _, err := svc.Create(ctx, service.CreateBookingInput{
		RoomID: room.ID, UserID: first.ID, Start: start, End: end, AttendeeCount: 1,
	})
	if err != nil {
		t.Fatalf("first booking: %v", err)
	}

	// Same slot while the first booking stands: must be refused.
	if _, _, err := svc.Create(ctx, service.CreateBookingInput{
		RoomID: room.ID, UserID: second.ID, Start: start, End: end, AttendeeCount: 1,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("booking an occupied slot: got %v, want ErrConflict", err)
	}

	if err := svc.Cancel(ctx, booking.ID, first.ID, false); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Same slot again, now that it is free.
	if _, _, err := svc.Create(ctx, service.CreateBookingInput{
		RoomID: room.ID, UserID: second.ID, Start: start, End: end, AttendeeCount: 1,
	}); err != nil {
		t.Errorf("booking the slot a cancellation freed: %v", err)
	}
}

// TestCreate_IdempotencyKeyReplay checks that a resubmitted request returns the
// original booking instead of making a second one.
//
// This is the double-click case. Without it the member ends up with two
// identical bookings — or, because the two would overlap, one booking and a
// confusing 409 for a request that in fact succeeded.
func TestCreate_IdempotencyKeyReplay(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	room := testutil.Room(t, db, 4)
	user := testutil.User(t, db, domain.RoleMember)
	start, end := slot(2)
	key := uuid.NewString()

	in := service.CreateBookingInput{
		RoomID: room.ID, UserID: user.ID, Start: start, End: end,
		AttendeeCount: 1, IdempotencyKey: &key,
	}

	first, replayed, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if replayed {
		t.Error("first submit reported as a replay")
	}

	second, replayed, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("second submit with the same key: %v", err)
	}
	if !replayed {
		t.Error("second submit not reported as a replay")
	}
	if second.ID != first.ID {
		t.Errorf("replay returned booking %s, want the original %s", second.ID, first.ID)
	}

	var stored int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE room_id = $1`, room.ID).Scan(&stored); err != nil {
		t.Fatalf("count stored bookings: %v", err)
	}
	if stored != 1 {
		t.Errorf("database holds %d bookings after a replayed submit, want 1", stored)
	}
}

// TestCreate_ConcurrentIdempotencyKeyReplay is the double-click as it actually
// happens: two requests in flight at once, not one after the other.
//
// The sequential test above passes on a lookup-then-insert implementation
// because the first insert has committed before the second request looks. Here
// both look before either writes, so the unique index on
// (user_id, idempotency_key) is what has to catch it. Both callers must end up
// with the same booking and there must be exactly one row.
func TestCreate_ConcurrentIdempotencyKeyReplay(t *testing.T) {
	svc, db := newBookingService(t)
	ctx := context.Background()

	room := testutil.Room(t, db, 4)
	user := testutil.User(t, db, domain.RoleMember)
	start, end := slot(2)
	key := uuid.NewString()

	const attempts = 4

	var (
		release  = make(chan struct{})
		ready    sync.WaitGroup
		finished sync.WaitGroup

		mu     sync.Mutex
		ids    = map[uuid.UUID]int{}
		errs   []error
	)

	ready.Add(attempts)
	finished.Add(attempts)

	for i := 0; i < attempts; i++ {
		go func() {
			defer finished.Done()
			ready.Done()
			<-release

			b, _, err := svc.Create(ctx, service.CreateBookingInput{
				RoomID: room.ID, UserID: user.ID, Start: start, End: end,
				AttendeeCount: 1, IdempotencyKey: &key,
			})

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			ids[b.ID]++
		}()
	}

	ready.Wait()
	close(release)
	finished.Wait()

	// A conflict here is tolerable and a distinct booking is not. Losing the
	// race on the unique index surfaces as ErrConflict, and the client retries;
	// two bookings from one intended action is the bug.
	for _, err := range errs {
		if !errors.Is(err, domain.ErrConflict) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if len(ids) > 1 {
		t.Errorf("concurrent replays produced %d distinct bookings, want 1", len(ids))
	}

	var stored int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE room_id = $1`, room.ID).Scan(&stored); err != nil {
		t.Fatalf("count stored bookings: %v", err)
	}
	if stored != 1 {
		t.Errorf("database holds %d bookings after concurrent replays, want 1", stored)
	}
}
