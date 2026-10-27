package httpapi

import (
	"net/http"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
	"github.com/nikitaredbee-lang/task-service/internal/service"
)

type createTeamRequest struct {
	Name string `json:"name"`
}

func (h *Handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var req createTeamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	team, err := h.Teams.Create(r.Context(), userIDFrom(r.Context()), req.Name)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, team)
}

func (h *Handler) listTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := h.Teams.List(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": teams})
}

type inviteRequest struct {
	Email string      `json:"email"`
	Role  domain.Role `json:"role"`
}

func (h *Handler) inviteMember(w http.ResponseWriter, r *http.Request) {
	teamID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var req inviteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	member, err := h.Teams.Invite(r.Context(), userIDFrom(r.Context()), teamID, service.InviteInput{
		Email: req.Email,
		Role:  req.Role,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, member)
}

type changeRoleRequest struct {
	Role domain.Role `json:"role"`
}

func (h *Handler) changeMemberRole(w http.ResponseWriter, r *http.Request) {
	teamID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	targetID, err := pathID(r, "userID")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var req changeRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := h.Teams.ChangeRole(r.Context(), userIDFrom(r.Context()), teamID, targetID, req.Role); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) teamStats(w http.ResponseWriter, r *http.Request) {
	teamID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	stats, err := h.Teams.Stats(r.Context(), userIDFrom(r.Context()), teamID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
