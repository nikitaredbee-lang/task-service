//go:build integration

package repository

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
	"github.com/nikitaredbee-lang/task-service/internal/service"
)

type noTaskCache struct{}

func (noTaskCache) Get(context.Context, domain.TaskFilter) (*domain.TaskPage, string, error) {
	return nil, "", nil
}
func (noTaskCache) Set(context.Context, domain.TaskFilter, string, *domain.TaskPage) error {
	return nil
}
func (noTaskCache) Invalidate(context.Context, int64) error { return nil }

type failingHistory struct{ service.TaskRepository }

func (failingHistory) AddHistory(context.Context, *domain.HistoryEntry) error {
	return errors.New("history write failed")
}

func TestTaskTransactions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seed := seeder{t: t, db: db, now: time.Now().UTC()}
	userID := seed.user("owner@example.com", "Owner")
	teamID := seed.team("Team", userID)
	tasks := NewTaskRepo(db)
	teams := NewTeamRepo(db)
	txm := NewTxManager(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewTaskService(txm, tasks, teams, noTaskCache{}, log)
	task, err := svc.Create(ctx, userID, service.CreateTaskInput{TeamID: teamID, Title: "initial"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("one winner for concurrent updates", func(t *testing.T) {
		const writers = 8
		start := make(chan struct{})
		results := make(chan error, writers)
		var wg sync.WaitGroup
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				title := "changed"
				_, err := svc.Update(ctx, userID, task.ID, service.UpdateTaskInput{Version: 1, Title: &title})
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		wins, conflicts := 0, 0
		for err := range results {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, domain.ErrConflict):
				conflicts++
			default:
				t.Errorf("unexpected update error: %v", err)
			}
		}
		if wins != 1 || conflicts != writers-1 {
			t.Fatalf("wins=%d, conflicts=%d", wins, conflicts)
		}
		current, err := tasks.Get(ctx, task.ID)
		if err != nil || current.Version != 2 || current.Title != "changed" {
			t.Fatalf("unexpected task: %+v, err=%v", current, err)
		}
		history, err := tasks.History(ctx, task.ID)
		if err != nil || len(history) != 2 {
			t.Fatalf("unexpected history: %+v, err=%v", history, err)
		}
		change := history[1].Changes["title"]
		if change.Old != "initial" || change.New != "changed" || history[1].ChangedBy != userID {
			t.Fatalf("unexpected history change: %+v", history[1])
		}
	})

	t.Run("history failure rolls back task", func(t *testing.T) {
		broken := service.NewTaskService(txm, failingHistory{tasks}, teams, noTaskCache{}, log)
		title := "must roll back"
		if _, err := broken.Update(ctx, userID, task.ID, service.UpdateTaskInput{Version: 2, Title: &title}); err == nil {
			t.Fatal("expected history failure")
		}
		current, err := tasks.Get(ctx, task.ID)
		if err != nil || current.Version != 2 || current.Title != "changed" {
			t.Fatalf("failed update persisted: %+v, err=%v", current, err)
		}
		if _, err := broken.Create(ctx, userID, service.CreateTaskInput{TeamID: teamID, Title: title}); err == nil {
			t.Fatal("expected creation history failure")
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("failed create persisted: count=%d, err=%v", count, err)
		}
	})
}

func TestSafeTestDatabase(t *testing.T) {
	for name, want := range map[string]bool{
		"tasks_test": true, "another_123_test": true,
		"tasks": false, "": false, "tasks_test`": false, "tasks-test": false,
	} {
		if got := safeTestDatabase(name); got != want {
			t.Errorf("safeTestDatabase(%q) = %v, want %v", name, got, want)
		}
	}
}
