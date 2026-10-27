package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

const (
	defaultLimit = 20
	maxLimit     = 100

	maxTitleLen       = 255
	maxDescriptionLen = 10000
	maxCommentLen     = 5000
)

type TaskService struct {
	tx    TxManager
	tasks TaskRepository
	teams TeamRepository
	cache TaskListCache
	log   *slog.Logger
}

func NewTaskService(tx TxManager, tasks TaskRepository, teams TeamRepository, cache TaskListCache, log *slog.Logger) *TaskService {
	return &TaskService{tx: tx, tasks: tasks, teams: teams, cache: cache, log: log}
}

type CreateTaskInput struct {
	TeamID      int64
	Title       string
	Description string
	Status      domain.TaskStatus
	AssigneeID  *int64
}

func (s *TaskService) Create(ctx context.Context, userID int64, in CreateTaskInput) (*domain.Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	if err := validateTitle(in.Title); err != nil {
		return nil, err
	}
	if err := validateDescription(in.Description); err != nil {
		return nil, err
	}
	if in.Status == "" {
		in.Status = domain.StatusTodo
	}
	if !in.Status.Valid() {
		return nil, domain.Invalid("status must be one of todo, in_progress, done")
	}

	created := now()
	task := &domain.Task{
		TeamID:      in.TeamID,
		Title:       in.Title,
		Description: in.Description,
		Status:      in.Status,
		CreatedBy:   userID,
		AssigneeID:  in.AssigneeID,
		CreatedAt:   created,
		UpdatedAt:   created,
		Version:     1,
	}
	if task.Status == domain.StatusDone {
		task.ClosedAt = &created
	}

	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.teamRole(ctx, in.TeamID, userID); err != nil {
			return err
		}
		if in.AssigneeID != nil {
			if err := s.ensureAssignee(ctx, in.TeamID, *in.AssigneeID); err != nil {
				return err
			}
		}
		if err := s.tasks.Create(ctx, task); err != nil {
			return err
		}

		// при создании пишем в историю все поля со старым значением null
		changes := map[string]domain.FieldChange{
			fieldTitle:       {New: task.Title},
			fieldDescription: {New: task.Description},
			fieldStatus:      {New: task.Status},
		}
		if task.AssigneeID != nil {
			changes[fieldAssignee] = domain.FieldChange{New: *task.AssigneeID}
		}
		if task.ClosedAt != nil {
			changes[fieldClosedAt] = domain.FieldChange{New: *task.ClosedAt}
		}
		return s.tasks.AddHistory(ctx, &domain.HistoryEntry{
			TaskID:    task.ID,
			ChangedBy: userID,
			Changes:   changes,
			CreatedAt: created,
		})
	})
	if err != nil {
		return nil, err
	}

	s.invalidateList(ctx, task.TeamID)
	return task, nil
}

func (s *TaskService) List(ctx context.Context, userID int64, f domain.TaskFilter) (*domain.TaskPage, error) {
	if f.TeamID <= 0 {
		return nil, domain.Invalid("team_id is required")
	}
	if f.Status != "" && !f.Status.Valid() {
		return nil, domain.Invalid("status must be one of todo, in_progress, done")
	}
	if f.Limit == 0 {
		f.Limit = defaultLimit
	}
	if f.Limit < 0 || f.Limit > maxLimit {
		return nil, domain.Invalid(fmt.Sprintf("limit must be from 1 to %d", maxLimit))
	}
	if f.Offset < 0 {
		return nil, domain.Invalid("offset must not be negative")
	}

	// права проверяем до кэша, иначе чужой пользователь получил бы закешированный список
	if _, err := s.teamRole(ctx, f.TeamID, userID); err != nil {
		return nil, err
	}

	page, gen, err := s.cache.Get(ctx, f)
	cacheOK := err == nil
	if err != nil {
		s.log.WarnContext(ctx, "task list cache get failed", slog.Any("error", err))
	}
	if page != nil {
		return page, nil
	}

	items, total, err := s.tasks.List(ctx, f)
	if err != nil {
		return nil, err
	}
	page = &domain.TaskPage{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset}

	if cacheOK {
		if err := s.cache.Set(ctx, f, gen, page); err != nil {
			s.log.WarnContext(ctx, "task list cache set failed", slog.Any("error", err))
		}
	}
	return page, nil
}

func (s *TaskService) Get(ctx context.Context, userID, taskID int64) (*domain.Task, error) {
	return s.visibleTask(ctx, userID, taskID)
}

type UpdateTaskInput struct {
	// Version это версия задачи, которую видел клиент. Если в базе уже другая, вернем 409.
	Version     int
	Title       *string
	Description *string
	Status      *domain.TaskStatus
	// AssigneeSet true, если assignee_id был в запросе. AssigneeID == nil при этом
	// значит снять исполнителя.
	AssigneeSet bool
	AssigneeID  *int64
}

func (s *TaskService) Update(ctx context.Context, userID, taskID int64, in UpdateTaskInput) (*domain.Task, error) {
	if in.Version <= 0 {
		return nil, domain.Invalid("version is required")
	}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if err := validateTitle(title); err != nil {
			return nil, err
		}
		in.Title = &title
	}
	if in.Description != nil {
		if err := validateDescription(*in.Description); err != nil {
			return nil, err
		}
	}
	if in.Status != nil && !in.Status.Valid() {
		return nil, domain.Invalid("status must be one of todo, in_progress, done")
	}

	var (
		result  *domain.Task
		changed bool
	)
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		// SELECT ... FOR UPDATE: параллельный апдейт этой же задачи будет ждать нашего коммита,
		// а дифф для истории считается от того, что реально лежит в базе.
		cur, err := s.tasks.GetForUpdate(ctx, taskID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.NotFound("task not found")
		}
		if err != nil {
			return err
		}

		role, err := s.teams.MemberRole(ctx, cur.TeamID, userID)
		if errors.Is(err, domain.ErrNotFound) {
			// задача из чужой команды, делаем вид что ее нет
			return domain.NotFound("task not found")
		}
		if err != nil {
			return err
		}

		// Блокировка сериализует запись, но не защищает от устаревшей версии клиента.
		if in.Version != cur.Version {
			return domain.Conflict(fmt.Sprintf(
				"task was changed by someone else, current version is %d", cur.Version))
		}

		next, changes := applyChanges(cur, in, now())
		if err := checkTaskEdit(role, userID, cur, editedFields(changes)); err != nil {
			return err
		}
		if len(changes) == 0 {
			result = cur
			return nil
		}

		if _, ok := changes[fieldAssignee]; ok && next.AssigneeID != nil {
			if err := s.ensureAssignee(ctx, cur.TeamID, *next.AssigneeID); err != nil {
				return err
			}
		}

		if err := s.tasks.Update(ctx, next, cur.Version); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return domain.Conflict("task was changed by someone else, reload it and try again")
			}
			return err
		}
		if err := s.tasks.AddHistory(ctx, &domain.HistoryEntry{
			TaskID:    next.ID,
			ChangedBy: userID,
			Changes:   changes,
			CreatedAt: next.UpdatedAt,
		}); err != nil {
			return err
		}

		result = next
		changed = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if changed {
		s.invalidateList(ctx, result.TeamID)
	}
	return result, nil
}

func (s *TaskService) History(ctx context.Context, userID, taskID int64) ([]domain.HistoryEntry, error) {
	if _, err := s.visibleTask(ctx, userID, taskID); err != nil {
		return nil, err
	}
	return s.tasks.History(ctx, taskID)
}

func (s *TaskService) AddComment(ctx context.Context, userID, taskID int64, content string) (*domain.Comment, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > maxCommentLen {
		return nil, domain.Invalid(fmt.Sprintf("content must be from 1 to %d characters", maxCommentLen))
	}
	if _, err := s.visibleTask(ctx, userID, taskID); err != nil {
		return nil, err
	}

	c := &domain.Comment{
		TaskID:    taskID,
		UserID:    userID,
		Content:   content,
		CreatedAt: now(),
	}
	if err := s.tasks.AddComment(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *TaskService) Comments(ctx context.Context, userID, taskID int64, limit, offset int) ([]domain.Comment, error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 0 || limit > maxLimit {
		return nil, domain.Invalid(fmt.Sprintf("limit must be from 1 to %d", maxLimit))
	}
	if offset < 0 {
		return nil, domain.Invalid("offset must not be negative")
	}
	if _, err := s.visibleTask(ctx, userID, taskID); err != nil {
		return nil, err
	}
	return s.tasks.Comments(ctx, taskID, limit, offset)
}

// visibleTask достает задачу и проверяет, что пользователь состоит в ее команде.
// Для чужих задач возвращаем 404, а не 403.
func (s *TaskService) visibleTask(ctx context.Context, userID, taskID int64) (*domain.Task, error) {
	task, err := s.tasks.Get(ctx, taskID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.NotFound("task not found")
	}
	if err != nil {
		return nil, err
	}
	_, err = s.teams.MemberRole(ctx, task.TeamID, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.NotFound("task not found")
	}
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (s *TaskService) teamRole(ctx context.Context, teamID, userID int64) (domain.Role, error) {
	role, err := s.teams.MemberRole(ctx, teamID, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.NotFound("team not found")
	}
	return role, err
}

func (s *TaskService) ensureAssignee(ctx context.Context, teamID, assigneeID int64) error {
	_, err := s.teams.MemberRole(ctx, teamID, assigneeID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Invalid("assignee must be a member of the team")
	}
	return err
}

func (s *TaskService) invalidateList(ctx context.Context, teamID int64) {
	// Коммит уже прошел. Даже если клиент оборвал запрос инвалидацию надо сделать.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.cache.Invalidate(ctx, teamID); err != nil {
		// Не валим запрос: изменение уже в базе. В худшем случае список
		// будет старым до истечения TTL.
		s.log.ErrorContext(ctx, "task list cache invalidate failed",
			slog.Int64("team_id", teamID), slog.Any("error", err))
	}
}

// одинаковые значения не попадают в историю и version не увеличивают
func applyChanges(cur *domain.Task, in UpdateTaskInput, at time.Time) (*domain.Task, map[string]domain.FieldChange) {
	next := *cur
	changes := make(map[string]domain.FieldChange)

	if in.Title != nil && *in.Title != cur.Title {
		changes[fieldTitle] = domain.FieldChange{Old: cur.Title, New: *in.Title}
		next.Title = *in.Title
	}
	if in.Description != nil && *in.Description != cur.Description {
		changes[fieldDescription] = domain.FieldChange{Old: cur.Description, New: *in.Description}
		next.Description = *in.Description
	}
	if in.Status != nil && *in.Status != cur.Status {
		changes[fieldStatus] = domain.FieldChange{Old: cur.Status, New: *in.Status}
		next.Status = *in.Status

		switch {
		case next.Status == domain.StatusDone:
			next.ClosedAt = &at
			changes[fieldClosedAt] = domain.FieldChange{Old: cur.ClosedAt, New: at}
		case cur.Status == domain.StatusDone:
			next.ClosedAt = nil
			changes[fieldClosedAt] = domain.FieldChange{Old: cur.ClosedAt, New: nil}
		}
	}
	if in.AssigneeSet && !sameID(cur.AssigneeID, in.AssigneeID) {
		changes[fieldAssignee] = domain.FieldChange{Old: cur.AssigneeID, New: in.AssigneeID}
		next.AssigneeID = in.AssigneeID
	}

	if len(changes) > 0 {
		next.Version = cur.Version + 1
		next.UpdatedAt = at
	}
	return &next, changes
}

// editedFields возвращает поля, которые поменял пользователь. closed_at выставляется
// автоматически вместе со статусом, поэтому в проверку прав не идет.
func editedFields(changes map[string]domain.FieldChange) []string {
	fields := make([]string, 0, len(changes))
	for f := range changes {
		if f != fieldClosedAt {
			fields = append(fields, f)
		}
	}
	return fields
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func validateTitle(title string) error {
	if title == "" || utf8.RuneCountInString(title) > maxTitleLen {
		return domain.Invalid(fmt.Sprintf("title must be from 1 to %d characters", maxTitleLen))
	}
	return nil
}

func validateDescription(d string) error {
	if utf8.RuneCountInString(d) > maxDescriptionLen {
		return domain.Invalid(fmt.Sprintf("description must be at most %d characters", maxDescriptionLen))
	}
	return nil
}
