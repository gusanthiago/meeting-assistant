package api

import (
	"context"
	"net/http"
	"time"

	"meeting-assistant/backend/api/gen"
	"meeting-assistant/backend/api/response"
)

const readinessTimeout = 3 * time.Second

// GetHealth reports that the process is up.
func (h *Handler) GetHealth(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, gen.Health{Status: gen.Ok})
}

// GetReadiness checks every dependency and answers 503 when one is unreachable.
func (h *Handler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := map[string]string{}
	health := gen.Health{Status: gen.Ok, Checks: &checks}
	for name, check := range h.checks {
		checks[name] = "ok"
		if err := check(ctx); err != nil {
			checks[name] = err.Error()
			health.Status = gen.Unavailable
		}
	}

	status := http.StatusOK
	if health.Status != gen.Ok {
		status = http.StatusServiceUnavailable
	}
	response.JSON(w, status, health)
}
