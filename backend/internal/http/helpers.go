package http

import (
	"time"

	"atrium/internal/config"
	"atrium/internal/domain"
)

// canCheckInNow decides whether the button should be enabled, so every client
// computes it identically — and so the client can defer to the server's clock
// instead of trusting its own when they disagree.
func canCheckInNow(b domain.Booking, now time.Time) bool {
	if b.Status != domain.BookingStatusConfirmed {
		return false
	}
	if b.CheckedInAt != nil {
		return false
	}
	if now.Before(b.StartTime.Add(-config.CheckInWindowBefore)) {
		return false
	}
	if now.After(b.StartTime.Add(config.CheckInGracePeriod)) {
		return false
	}
	return true
}
