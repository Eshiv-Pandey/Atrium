// Package domain defines the core business entities and domain errors.
//
// Domain errors (ErrNotFound, ErrConflict, etc.) are defined here and returned
// by the service layer. The HTTP layer maps them to status codes in exactly
// one place.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// User represents an account with authentication credentials and a role.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Name         string
	Role         Role
	CreatedAt    time.Time
}

type Role string

const (
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
)

// Room represents a bookable space with capacity and amenities.
type Room struct {
	ID        uuid.UUID
	Name      string
	Capacity  int
	Amenities []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Booking represents a reservation of a room by a user for a time range.
type Booking struct {
	ID             uuid.UUID
	RoomID         uuid.UUID
	UserID         uuid.UUID
	StartTime      time.Time
	EndTime        time.Time
	AttendeeCount  int
	Status         BookingStatus
	CheckedInAt    *time.Time
	ReleasedAt     *time.Time
	CancelledAt    *time.Time
	IdempotencyKey *string
	CreatedAt      time.Time
}

type BookingStatus string

const (
	BookingStatusConfirmed BookingStatus = "confirmed"
	BookingStatusCancelled BookingStatus = "cancelled"
	BookingStatusReleased  BookingStatus = "released"
)

// Domain errors. The HTTP layer catches these and maps them to status codes.
var (
	// ErrNotFound means the requested resource does not exist. Maps to 404.
	ErrNotFound = errors.New("not found")

	// ErrConflict means the operation would violate a uniqueness or overlap
	// constraint. Maps to 409. Most commonly: booking an occupied slot.
	ErrConflict = errors.New("conflict")

	// ErrForbidden means the authenticated user lacks permission for the
	// operation. Maps to 403. Distinct from ErrUnauthorized (which is 401).
	ErrForbidden = errors.New("forbidden")

	// ErrUnauthorized means no valid credentials were provided. Maps to 401.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrValidation means the input failed business rule validation (not just
	// shape validation, which the HTTP layer already handles). Maps to 422.
	// Examples: booking in the past, attendee count exceeds room capacity.
	ErrValidation = errors.New("validation failed")
)
