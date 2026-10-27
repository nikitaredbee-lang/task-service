package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nikitaredbee-lang/task-service/internal/auth"
	"github.com/nikitaredbee-lang/task-service/internal/ratelimit"
	"github.com/nikitaredbee-lang/task-service/internal/service"
)

type Deps struct {
	Auth    *service.AuthService
	Teams   *service.TeamService
	Tasks   *service.TaskService
	Tokens  *auth.TokenManager
	Limiter *ratelimit.Limiter // nil если лимит выключен
	Log     *slog.Logger
	// Health проверяет зависимости, без которых API не может обслуживать запросы.
	Health func(ctx context.Context) error
}

type Handler struct {
	Deps
}

func NewRouter(d Deps) http.Handler {
	h := &Handler{Deps: d}

	r := chi.NewRouter()
	r.Use(h.requestID, h.accessLog, h.recoverer)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErrorCode(w, http.StatusNotFound, "not_found", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeErrorCode(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	r.Get("/health", h.health)
	r.Get("/swagger", http.RedirectHandler("/swagger/", http.StatusMovedPermanently).ServeHTTP)
	r.Get("/swagger/", swaggerUI)
	r.Get("/swagger/openapi.yaml", swaggerSpec)

	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(h.rateLimit(limitByIP))
			r.Post("/register", h.register)
			r.Post("/login", h.login)
		})

		r.Group(func(r chi.Router) {
			r.Use(h.authenticate, h.rateLimit(limitByUser))

			r.Post("/teams", h.createTeam)
			r.Get("/teams", h.listTeams)
			r.Post("/teams/{id}/invite", h.inviteMember)
			r.Put("/teams/{id}/members/{userID}", h.changeMemberRole)
			r.Get("/teams/{id}/stats", h.teamStats)

			r.Post("/tasks", h.createTask)
			r.Get("/tasks", h.listTasks)
			r.Get("/tasks/{id}", h.getTask)
			r.Put("/tasks/{id}", h.updateTask)
			r.Get("/tasks/{id}/history", h.taskHistory)
			r.Post("/tasks/{id}/comments", h.createComment)
			r.Get("/tasks/{id}/comments", h.listComments)
		})
	})

	return r
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if h.Health != nil {
		if err := h.Health(r.Context()); err != nil {
			h.Log.WarnContext(r.Context(), "health check failed", slog.Any("error", err))
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
