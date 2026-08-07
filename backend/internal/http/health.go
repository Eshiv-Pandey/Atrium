package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"atrium/internal/store"
)

// HealthHandler reports whether the service can do its job.
type HealthHandler struct {
	db *store.DB
}

func NewHealthHandler(db *store.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

// healthCheckTimeout bounds the database ping.
//
// Without it, a database that accepts connections but never answers would make
// the health check hang instead of failing — and a health check that hangs is
// worse than one that fails, because an orchestrator reads it as "still
// starting" rather than "broken".
const healthCheckTimeout = 2 * time.Second

// Check reports liveness together with database reachability.
//
// GET /api/healthz
//
// The ping is included on purpose: this endpoint backs the Compose
// healthcheck, and an API that is listening but cannot reach Postgres can
// serve nothing useful. Reporting it healthy would let `docker compose up`
// declare success on a stack that is broken.
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		// 503 rather than 500: the service itself is running, its dependency is
		// not, and the distinction is what tells an operator where to look.
		//
		// The error is logged rather than returned. A health endpoint is
		// typically unauthenticated, and a connection error string names the
		// host, port, and user we connect as.
		slog.ErrorContext(r.Context(), "health check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status:   "degraded",
			Database: "unreachable",
		})
		return
	}

	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Database: "ok"})
}
