# Concurrency

The hard part of a booking system is not the create-read-update-delete. It is what happens when two people reach for the same room at the same second. Atrium settles that in the database with a PostgreSQL exclusion constraint rather than with application-level locking, and almost every other design decision falls out of that one choice. If you read only one page of these docs, read this one.

## The core invariant

The most important lines in the whole repository are in `backend/migrations/000001_init.up.sql`:

```sql
ALTER TABLE bookings
    ADD CONSTRAINT bookings_no_overlap
    EXCLUDE USING gist (
        room_id WITH =,
        tstzrange(start_time, end_time, '[)') WITH &&
    )
    WHERE (status = 'confirmed');
```

Read it as a sentence: for confirmed bookings, no two rows may share the same `room_id` and have overlapping time ranges. Two overlapping confirmed bookings for one room cannot exist. Not "are unlikely," not "are guarded against by careful code." They cannot exist, because the database will not store the second one.

That single guarantee is what the rest of this page unpacks. Three consequences follow from it, and then one subtle piece is needed to make it behave well under real load.

## Consequence one: no check-then-insert

The obvious way to build this is read-then-write in application code: `SELECT` to check the slot is free, then `INSERT` if it is. That is a lost-update race, and no amount of care in Go can close it. Two concurrent requests both run the `SELECT`, both see the slot free, and both run the `INSERT`. The gap between the two statements is where the bug lives, and the gap cannot be removed as long as they are two statements.

You could serialize on the room with `SELECT ... FOR UPDATE`, but that turns every booking into a per-room lock queue held across an availability read, and it has nothing to lock in the case that matters, since a free slot has no row to lock.

So the service does not check. `BookingService.Create` inserts directly, and the constraint accepts or rejects. The happy path is a single `INSERT` with no preceding read at all. A rejection comes back as SQLSTATE `23P01`, which the store recognizes with `IsOverlapConflict` and the handler turns into a 409. The check and the write are the same operation, which is the only way to make them atomic.

This is why the project's guidance is so firm about never adding a `SELECT` before the insert. A read beforehand does not make anything safer; it just adds a race window and a wasted round trip in front of the one operation that was already correct.

## Consequence two: no-show release is lazy, with no scheduler

A booking that nobody checks into within the grace period (15 minutes after its start) should stop holding its slot. The natural instinct is a cron job that sweeps the table and flips stale bookings to released. Atrium does not do that, and deliberately so.

Instead, release is lazy and happens inside the booking transaction. When someone tries to book a room, the transaction first flips any of that room's confirmed-but-never-checked-in bookings whose start time is more than the grace period in the past to `status = 'released'`. Because the exclusion constraint is predicated on `status = 'confirmed'`, flipping a row to `released` drops it out of the constraint's index, which frees the slot. The insert that follows in the same transaction can then take it.

The relevant statement is `ReleaseStaleNoShows` in `store/bookings.go`:

```sql
UPDATE bookings
SET status = 'released', released_at = now()
WHERE room_id = $1
  AND status = 'confirmed'
  AND checked_in_at IS NULL
  AND start_time + $2::interval < now()
```

Two things make this work without a scheduler. First, a stale slot is freed and refilled in one atomic operation, so there is never a window where the slot is free but not yet taken. Second, the same `status = 'confirmed'` predicate that the constraint uses also runs in every availability read, so a stale booking never appears to occupy a slot even before a write sweeps it. Read and write are the only two moments the invariant can be observed, and it holds at both.

The sweep is scoped to the room being booked, not the whole table. A stale booking in another room cannot block this insert, so there is no reason to pay for sweeping it. Sweeping every room on every booking would make each write proportional to the size of the whole table for no benefit.

The reason to avoid a second mechanism is drift. A cron job that releases no-shows would be a separate piece of logic that could fall out of sync with the in-transaction rule, and then the answer to "is this slot free?" would depend on which mechanism ran last. One mechanism cannot disagree with itself.

## Consequence three: the database clock decides state changes

Check-in and release both put their entire condition into SQL against `now()`, inside the `WHERE` clause of the `UPDATE`. The check-in statement, for instance, evaluates the whole window (not before the open time, not after the grace period, not already checked in) in the same statement that does the write. That means the check and the write cannot be split by a concurrent writer: a booking cannot be released by a sweep in the instant between a check-in reading it and updating it, because there is no such instant.

Go's `time.Now()` is used only in one place, and on purpose: the pre-flight validation in `BookingService.validate` that rejects a booking in the past or too far in the future. That is advisory validation producing a friendly 422, with a one-minute clock-skew tolerance so a client whose clock is a few seconds fast is not told its own "now" is already history. It is never the authority over stored state. The rule of thumb is: friendly rejection uses Go time; the actual state transition uses database time.

## The subtle piece: ordering, not deciding

Everything above is enough for correctness. It is not enough for the system to behave well when many requests genuinely contend for one slot, and the reason is a detail of how Postgres validates an exclusion constraint.

Postgres checks an exclusion constraint by inserting the index entry first and scanning for conflicts second. When several transactions all insert before any of them scans, each one finds the others in progress and waits on their transaction ids. That is a cycle with no winner. The only thing that can break it is the deadlock detector, which fires one victim per `deadlock_timeout`. The losers do not get a clean "that slot is taken"; they get an arbitrary deadlock abort, SQLSTATE `40P01`, which surfaces as a 500.

This was measured on this exact schema, with 8 requests racing for one slot:

| | Result | Time |
|---|---|---|
| Without the room lock | 1 success, 7 x `40P01` deadlock | 7.9s |
| With the room lock | 1 success, 7 x `23P01` conflict | 0.13s |

Both rows store exactly one booking. The invariant held either way; that was never in question. What changed was the answer the seven losers got and how long it took: an arbitrary deadlock surfaced as a 500 after nearly eight seconds, versus an honest "that slot is taken" in a tenth of a second.

The fix is `store.LockRoom`, which takes a transaction-scoped advisory lock on the room inside the booking transaction:

```sql
SELECT pg_advisory_xact_lock($1, hashtext($2::text))
```

It is worth being exact about what this lock does, because it is easy to mistake for the very thing the design avoids.

**It orders arrivals; it does not decide them.** The lock reads no bookings and makes no decision. Remove it and every booking outcome is identical, because `bookings_no_overlap` is still the only thing that accepts or rejects a row. All the lock changes is the order in which contenders reach the constraint: one at a time, so the first commits and the rest see a committed row and get a clean `23P01` instead of piling into a deadlock cycle.

**It is not `SELECT ... FOR UPDATE`.** That would hold a lock across an availability read, scale with the number of rows examined, and have nothing to lock in the case that matters, since a free slot has no row. This advisory lock covers a single `INSERT` and the one narrow no-show `UPDATE`, both sub-millisecond, and holds no lock across a read.

**Different rooms never contend.** The lock key is derived from the room id, so a booking for room A and a booking for room B proceed in parallel. Only requests for the same room queue, and only for the sub-millisecond it takes to insert. There is a test, `TestCreate_ConcurrentDistinctRoomsAllSucceed`, that guards this: it would fail if the lock key were ever widened to a constant.

## Why not just retry?

If losers get an abort, why not retry the transaction until it succeeds or gets a clean conflict? Because retrying into a contended exclusion constraint actively makes things worse. Each new attempt joins the pileup it is waiting on.

That too was measured: with retries and no room lock, the seven losers burned six attempts each and produced zero clean conflicts, because the winner could not commit until the deadlock detector had killed everyone ahead of it. The retries did not resolve the contention; they fed it.

So `WithTx` retries only two error codes, `40001` (serialization failure) and `40P01` (deadlock), and only as a backstop for incidental contention, such as two writers touching the same rows in opposite orders. It explicitly does not retry a constraint violation, because that is an answer, not an interruption: it would repeat identically every time, and retrying it would only delay the 409 the caller is owed. The room lock is what makes those retryable aborts rare in the first place, so the retry budget can stay small (three attempts, with jittered backoff).

## The transaction, start to finish

Putting it together, here is the exact sequence inside `BookingService.Create`, all in one transaction:

1. **Replay check.** If the request carried an `Idempotency-Key`, look for a booking this user already made with that key. If found, return it and stop. A double-submitted form does not double-book. This runs before the lock so a resubmitted form answers from the original booking without waiting on a busy room.
2. **Lock the room.** Take the advisory lock, so this is the only booking attempt for this room in flight. Ordering, not checking.
3. **Release stale no-shows for this room.** Free any slot held by a booking nobody turned up for, in the same transaction that is about to try to fill it.
4. **Insert.** No availability check. The exclusion constraint accepts the row or raises `23P01`, which becomes a 409.

Read that list next to the four rules above and every step has a reason. There is no `SELECT` before the insert, because the constraint is the check. The release is in the transaction, not a cron job, so a freed slot is refilled atomically. The lock orders arrivals so losers get a clean conflict instead of a deadlock. The whole thing is one transaction so nothing can wedge itself between the steps.

## The one test that carries this

`TestCreate_ConcurrentSameSlot` in `service/booking_concurrency_test.go` is the load-bearing test of the entire suite. It releases eight distinct users from a `sync.WaitGroup` barrier onto the same slot at once, then checks two things separately: that exactly one booking was stored, and that exactly seven callers got a conflict. The first assertion is about the invariant; the second is about the error contract. They fail for unrelated reasons and must be read separately. [Testing](testing.md#the-load-bearing-test) explains why the barrier, why eight users, and why both assertions.

## What not to do

These are the mistakes this design exists to avoid. Each one has been considered and rejected for the reasons above:

- Do not check availability and then insert. The constraint is the check; a `SELECT` first is a race window.
- Do not use `SELECT ... FOR UPDATE` to serialize bookings. The advisory lock is a different, narrower thing.
- Do not widen the room lock's key to a constant. Different rooms must not contend.
- Do not retry a booking transaction to make a conflict go away. Retrying into contention feeds it.
- Do not use `time.Now()` to decide a state transition. Database time is the authority; Go time is for advisory validation only.
- Do not add a cron job to release no-shows. The release is lazy and in-transaction, and a second mechanism would drift from the first.

