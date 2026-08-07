// Command seed populates a database with demo data.
//
// Seeding lives here rather than in a migration for one concrete reason:
// argon2id embeds a random salt in every hash, so a password hash cannot be
// written into a .sql file by hand — it has to be produced by the same hasher
// the login path uses. Beyond that, seed data is not schema: a migration is a
// statement about structure, and mixing sample rows into one makes the schema
// history harder to read and impossible to apply to a real database.
//
// Idempotent by design: it can be run against a database that already has data
// and will neither duplicate nor overwrite. `docker compose up` runs it every
// time, and a reviewer restarting the stack should not accumulate five copies
// of the same room.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atrium/internal/auth"
	"atrium/internal/config"
	"atrium/internal/domain"
	"atrium/internal/store"
)

// demoUser is a seeded account. Passwords are deliberately weak and printed in
// the README: this is a take-home reviewer's entry point, not a production
// system, and the demo-login route that uses these is off unless explicitly
// enabled.
type demoUser struct {
	Email    string
	Password string
	Name     string
	Role     domain.Role
}

var demoUsers = []demoUser{
	{Email: "admin@atrium.local", Password: "admin123", Name: "Avery Admin", Role: domain.RoleAdmin},
	{Email: "member@atrium.local", Password: "member123", Name: "Morgan Member", Role: domain.RoleMember},
}

// demoRooms covers the range the UI needs to look real: a room too small for
// most meetings, two mid-size rooms that differ only by amenities (so the
// amenity filter has something to discriminate), and one large room.
var demoRooms = []domain.Room{
	{Name: "Phone Booth", Capacity: 1, Amenities: []string{"quiet"}},
	{Name: "Focus Pod", Capacity: 2, Amenities: []string{"quiet", "whiteboard"}},
	{Name: "Conference A", Capacity: 6, Amenities: []string{"tv", "whiteboard"}},
	{Name: "Open Lounge", Capacity: 8, Amenities: []string{"casual", "tv"}},
	{Name: "Board Room", Capacity: 12, Amenities: []string{"videoconf", "tv", "whiteboard"}},
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	users := store.NewUserStore(db)
	rooms := store.NewRoomStore(db)
	bookings := store.NewBookingStore(db)
	hasher := auth.NewPasswordHasher()

	userIDs, err := seedUsers(ctx, users, hasher)
	if err != nil {
		return err
	}

	roomIDs, err := seedRooms(ctx, rooms)
	if err != nil {
		return err
	}

	if err := seedBookings(ctx, db, bookings, userIDs, roomIDs); err != nil {
		return err
	}

	slog.Info("seed complete")
	return nil
}

func seedUsers(ctx context.Context, users *store.UserStore, hasher *auth.PasswordHasher) (map[string]uuid.UUID, error) {
	ids := make(map[string]uuid.UUID, len(demoUsers))

	for _, u := range demoUsers {
		// Look first. Re-hashing an existing user's password would produce a
		// different hash for the same password and, worse, would overwrite one
		// a reviewer might have changed.
		existing, err := users.GetByEmail(ctx, u.Email)
		if err == nil {
			ids[u.Email] = existing.ID
			slog.Info("user exists, skipping", "email", u.Email)
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}

		hash, err := hasher.Hash(u.Password)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", u.Email, err)
		}

		created, err := users.Create(ctx, &domain.User{
			Email:        u.Email,
			PasswordHash: hash,
			Name:         u.Name,
			Role:         u.Role,
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", u.Email, err)
		}

		ids[u.Email] = created.ID
		slog.Info("user created", "email", u.Email, "role", u.Role)
	}
	return ids, nil
}

func seedRooms(ctx context.Context, rooms *store.RoomStore) (map[string]uuid.UUID, error) {
	existing, err := rooms.List(ctx, store.RoomFilter{})
	if err != nil {
		return nil, err
	}

	ids := make(map[string]uuid.UUID, len(demoRooms))
	for _, r := range existing {
		ids[r.Name] = r.ID
	}

	for _, r := range demoRooms {
		if _, ok := ids[r.Name]; ok {
			slog.Info("room exists, skipping", "name", r.Name)
			continue
		}

		created, err := rooms.Create(ctx, &domain.Room{
			Name:      r.Name,
			Capacity:  r.Capacity,
			Amenities: r.Amenities,
		})
		if err != nil {
			return nil, fmt.Errorf("create room %s: %w", r.Name, err)
		}

		ids[r.Name] = created.ID
		slog.Info("room created", "name", r.Name, "capacity", r.Capacity)
	}
	return ids, nil
}

// seedBookings creates demo reservations relative to now.
//
// Times are relative rather than absolute so the seed data is always
// meaningful: a fixed date would be in the past by the time anyone reviewed it,
// and "my upcoming bookings" would render empty on the screen it is meant to
// demonstrate.
//
// The set is chosen so every state the UI can show has an example:
//   - an upcoming booking, cancellable
//   - a booking starting shortly, inside its check-in window
//   - a past booking that was attended
//   - a past booking nobody checked into, which the release sweep will free
func seedBookings(
	ctx context.Context,
	db *store.DB,
	bookings *store.BookingStore,
	userIDs map[string]uuid.UUID,
	roomIDs map[string]uuid.UUID,
) error {
	member, ok := userIDs["member@atrium.local"]
	if !ok {
		return errors.New("member account missing; cannot seed bookings")
	}

	// A marker key doubles as the idempotency guard: the partial unique index
	// on (user_id, idempotency_key) means a second run inserts nothing rather
	// than piling up duplicates.
	type spec struct {
		room     string
		start    time.Time
		duration time.Duration
		key      string
		attended bool
	}

	now := time.Now().UTC().Truncate(time.Hour)
	specs := []spec{
		{room: "Conference A", start: now.Add(26 * time.Hour), duration: time.Hour, key: "seed-upcoming"},
		{room: "Focus Pod", start: now.Add(50 * time.Hour), duration: 90 * time.Minute, key: "seed-upcoming-2"},
		{room: "Board Room", start: now.Add(-48 * time.Hour), duration: 2 * time.Hour, key: "seed-past-attended", attended: true},
		{room: "Open Lounge", start: now.Add(-24 * time.Hour), duration: time.Hour, key: "seed-past-noshow"},
	}

	for _, s := range specs {
		roomID, ok := roomIDs[s.room]
		if !ok {
			return fmt.Errorf("room %q missing; cannot seed bookings", s.room)
		}

		key := s.key
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if _, err := bookings.FindByIdempotencyKey(ctx, tx, member, key); err == nil {
				slog.Info("booking exists, skipping", "key", key)
				return nil
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}

			_, err := bookings.Insert(ctx, tx, &domain.Booking{
				RoomID:         roomID,
				UserID:         member,
				StartTime:      s.start,
				EndTime:        s.start.Add(s.duration),
				AttendeeCount:  1,
				IdempotencyKey: &key,
			})
			if err != nil {
				return err
			}
			slog.Info("booking created", "room", s.room, "start", s.start.Format(time.RFC3339))
			return nil
		})
		if err != nil {
			return fmt.Errorf("seed booking %s: %w", s.key, err)
		}

		if s.attended {
			if err := markAttended(ctx, db, member, key); err != nil {
				return err
			}
		}
	}
	return nil
}

// markAttended backdates a check-in on a past booking.
//
// The CheckIn store method deliberately refuses writes outside the check-in
// window, which is correct for the API and unusable for seeding history. This
// writes the column directly rather than weakening that rule with a bypass
// parameter that would then exist in production code for the sake of a fixture.
func markAttended(ctx context.Context, db *store.DB, userID uuid.UUID, key string) error {
	const q = `
		UPDATE bookings SET checked_in_at = start_time
		WHERE user_id = $1 AND idempotency_key = $2 AND checked_in_at IS NULL`

	if _, err := db.Pool().Exec(ctx, q, userID, key); err != nil {
		return fmt.Errorf("mark attended %s: %w", key, err)
	}
	return nil
}
