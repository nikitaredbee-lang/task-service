package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

type directTx struct{}

func (directTx) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type taskStub struct {
	TaskRepository
	task    *domain.Task
	history *domain.HistoryEntry
}

func (r *taskStub) GetForUpdate(context.Context, int64) (*domain.Task, error) { return r.task, nil }
func (r *taskStub) Create(_ context.Context, task *domain.Task) error {
	task.ID = 1
	r.task = task
	return nil
}
func (r *taskStub) AddHistory(_ context.Context, h *domain.HistoryEntry) error {
	r.history = h
	return nil
}

type memberStub struct {
	TeamRepository
	err error
}

func (r memberStub) MemberRole(context.Context, int64, int64) (domain.Role, error) {
	return domain.RoleMember, r.err
}

type cacheStub struct {
	TaskListCache
	invalidated bool
	cancelled   bool
}

func (c *cacheStub) Invalidate(ctx context.Context, _ int64) error {
	c.invalidated = true
	c.cancelled = ctx.Err() != nil
	return nil
}

func TestUpdateUnrelatedMemberCannotSendNoop(t *testing.T) {
	tasks := &taskStub{task: &domain.Task{ID: 1, TeamID: 1, CreatedBy: 10, Title: "same", Version: 1}}
	svc := NewTaskService(directTx{}, tasks, memberStub{}, nil, nil)
	_, err := svc.Update(context.Background(), 20, 1, UpdateTaskInput{Version: 1, Title: ptr("same")})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestCreateDoneTaskRecordsClosedAt(t *testing.T) {
	tasks := &taskStub{}
	cache := &cacheStub{}
	svc := NewTaskService(directTx{}, tasks, memberStub{}, cache, slog.New(slog.NewTextHandler(io.Discard, nil)))
	task, err := svc.Create(context.Background(), 10, CreateTaskInput{TeamID: 1, Title: "done", Status: domain.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	change, ok := tasks.history.Changes[fieldClosedAt]
	if !ok || change.Old != nil || task.ClosedAt == nil || change.New != *task.ClosedAt {
		t.Fatalf("closed_at missing from creation history: %+v", tasks.history)
	}
	if !cache.invalidated {
		t.Fatal("task creation must invalidate list cache")
	}
}

func TestListChecksMembershipBeforeCache(t *testing.T) {
	// Кеш тут nil. Если сервис полезет в него до проверки прав тест упадет.
	svc := NewTaskService(nil, nil, memberStub{err: domain.ErrNotFound}, nil, nil)
	_, err := svc.List(context.Background(), 10, domain.TaskFilter{TeamID: 1})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestInvalidateSurvivesRequestCancellation(t *testing.T) {
	cache := &cacheStub{}
	svc := NewTaskService(nil, nil, nil, cache, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.invalidateList(ctx, 1)
	if !cache.invalidated || cache.cancelled {
		t.Fatal("invalidation must run after a cancelled request")
	}
}

func TestRegisterRejectsOversizedEmail(t *testing.T) {
	svc := NewAuthService(nil, nil)
	_, err := svc.Register(context.Background(), RegisterInput{
		Email: strings.Repeat("a", 245) + "@example.com", Password: "password123", Name: "Alice",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
