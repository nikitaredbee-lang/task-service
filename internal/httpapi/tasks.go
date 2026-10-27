package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
	"github.com/nikitaredbee-lang/task-service/internal/service"
)

type createTaskRequest struct {
	TeamID      int64             `json:"team_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      domain.TaskStatus `json:"status"`
	AssigneeID  *int64            `json:"assignee_id"`
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	if req.TeamID <= 0 {
		h.writeError(w, r, domain.Invalid("team_id is required"))
		return
	}
	task, err := h.Tasks.Create(r.Context(), userIDFrom(r.Context()), service.CreateTaskInput{
		TeamID:      req.TeamID,
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		AssigneeID:  req.AssigneeID,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	teamID, err := queryID(r, "team_id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if teamID == nil {
		h.writeError(w, r, domain.Invalid("team_id is required"))
		return
	}
	assigneeID, err := queryID(r, "assignee_id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	page, err := h.Tasks.List(r.Context(), userIDFrom(r.Context()), domain.TaskFilter{
		TeamID:     *teamID,
		Status:     domain.TaskStatus(r.URL.Query().Get("status")),
		AssigneeID: assigneeID,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	task, err := h.Tasks.Get(r.Context(), userIDFrom(r.Context()), taskID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// optionalID различает "поле не передали" и "передали null".
// Для assignee_id это важно: null значит снять исполнителя, а отсутствие поля не трогать его.
type optionalID struct {
	Set   bool
	Value *int64
}

func (o *optionalID) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

type updateTaskRequest struct {
	Version     int                `json:"version"`
	Title       *string            `json:"title"`
	Description *string            `json:"description"`
	Status      *domain.TaskStatus `json:"status"`
	AssigneeID  optionalID         `json:"assignee_id"`
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var req updateTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	if req.AssigneeID.Value != nil && *req.AssigneeID.Value <= 0 {
		h.writeError(w, r, domain.Invalid("assignee_id must be a positive integer or null"))
		return
	}

	task, err := h.Tasks.Update(r.Context(), userIDFrom(r.Context()), taskID, service.UpdateTaskInput{
		Version:     req.Version,
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		AssigneeSet: req.AssigneeID.Set,
		AssigneeID:  req.AssigneeID.Value,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) taskHistory(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	history, err := h.Tasks.History(r.Context(), userIDFrom(r.Context()), taskID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": history})
}

type createCommentRequest struct {
	Content string `json:"content"`
}

func (h *Handler) createComment(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var req createCommentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	comment, err := h.Tasks.AddComment(r.Context(), userIDFrom(r.Context()), taskID, req.Content)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id")
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	comments, err := h.Tasks.Comments(r.Context(), userIDFrom(r.Context()), taskID, limit, offset)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": comments})
}
