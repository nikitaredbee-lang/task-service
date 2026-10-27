//go:build integration

package repository

import (
	"context"
	"database/sql"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

// Интеграционный тест отчета по команде. Нужен живой MySQL:
//
//	TEST_MYSQL_DSN='root:root_password@tcp(localhost:3306)/tasks_test' go test -tags=integration ./internal/repository/...
//
// Тест сам создает базу и пересоздает в ней таблицы, поэтому не направляйте его на рабочую.
func TestTeamStats(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewTeamRepo(db)

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	s := seeder{t: t, db: db, now: now}
	owner := s.user("owner@example.com", "Owner")
	alice := s.user("alice@example.com", "Alice")
	bob := s.user("bob@example.com", "Bob")
	carol := s.user("carol@example.com", "Carol")
	dave := s.user("dave@example.com", "Dave")
	eve := s.user("eve@example.com", "Eve")

	teamA := s.team("Team A", owner)
	teamB := s.team("Team B", eve)
	emptyTeam := s.team("Empty", owner)

	// Команда A
	t1 := s.task(teamA, owner, nil, domain.StatusTodo, ago(day), nil)
	s.task(teamA, owner, &alice, domain.StatusInProgress, ago(day), nil)
	// закрытые за последние 30 дней: Alice 2, Bob 1, Carol 1, Dave 1
	t3 := s.task(teamA, owner, &alice, domain.StatusDone, ago(10*day), ptrTime(ago(9*day)))    // 1 день
	s.task(teamA, owner, &alice, domain.StatusDone, ago(5*day), ptrTime(ago(4*day)))           // 1 день
	s.task(teamA, owner, &bob, domain.StatusDone, ago(3*day), ptrTime(ago(day)))               // 2 дня
	s.task(teamA, owner, &carol, domain.StatusDone, ago(2*day), ptrTime(ago(2*day-time.Hour))) // 1 час
	s.task(teamA, owner, &dave, domain.StatusDone, ago(2*day), ptrTime(ago(day)))              // 1 день
	// закрыта 40 дней назад: в топ не идет, но в среднее время закрытия идет
	s.task(teamA, owner, &dave, domain.StatusDone, ago(50*day), ptrTime(ago(40*day))) // 10 дней

	s.comment(t1, owner)
	s.comment(t1, alice)
	s.comment(t1, bob)
	s.comment(t3, alice)

	// Команда B: у Eve закрытых задач больше всех, но в отчет команды A она попасть не должна
	for i := 0; i < 5; i++ {
		tb := s.task(teamB, eve, &eve, domain.StatusDone, ago(2*day), ptrTime(ago(day)))
		s.comment(tb, eve)
	}

	t.Run("team A", func(t *testing.T) {
		stats, err := repo.Stats(ctx, teamA, now)
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}

		wantStatus := map[domain.TaskStatus]int{
			domain.StatusTodo:       1,
			domain.StatusInProgress: 1,
			domain.StatusDone:       6,
		}
		for st, want := range wantStatus {
			if got := stats.TasksByStatus[st]; got != want {
				t.Errorf("tasks_by_status[%s] = %d, want %d", st, got, want)
			}
		}

		// при равном количестве выше тот, у кого меньше id (Bob и Carol раньше Dave)
		wantTop := []domain.AssigneeStat{
			{UserID: alice, Name: "Alice", ClosedTasks: 2},
			{UserID: bob, Name: "Bob", ClosedTasks: 1},
			{UserID: carol, Name: "Carol", ClosedTasks: 1},
		}
		if len(stats.TopAssignees) != len(wantTop) {
			t.Fatalf("top assignees = %+v, want %+v", stats.TopAssignees, wantTop)
		}
		for i := range wantTop {
			if stats.TopAssignees[i] != wantTop[i] {
				t.Errorf("top[%d] = %+v, want %+v", i, stats.TopAssignees[i], wantTop[i])
			}
		}

		// (1д + 1д + 2д + 1ч + 1д + 10д) / 6
		wantAvg := float64(86400+86400+172800+3600+86400+864000) / 6
		if stats.AvgCloseSeconds == nil {
			t.Fatal("avg_close_seconds is nil")
		}
		if math.Abs(*stats.AvgCloseSeconds-wantAvg) > 0.01 {
			t.Errorf("avg_close_seconds = %f, want %f", *stats.AvgCloseSeconds, wantAvg)
		}

		if stats.CommentsTotal != 4 {
			t.Errorf("comments_total = %d, want 4", stats.CommentsTotal)
		}
	})

	t.Run("team without tasks", func(t *testing.T) {
		stats, err := repo.Stats(ctx, emptyTeam, now)
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		for _, st := range domain.AllStatuses {
			if stats.TasksByStatus[st] != 0 {
				t.Errorf("tasks_by_status[%s] = %d, want 0", st, stats.TasksByStatus[st])
			}
		}
		if len(stats.TopAssignees) != 0 {
			t.Errorf("top assignees must be empty, got %+v", stats.TopAssignees)
		}
		if stats.AvgCloseSeconds != nil {
			t.Errorf("avg_close_seconds must be nil, got %f", *stats.AvgCloseSeconds)
		}
		if stats.CommentsTotal != 0 {
			t.Errorf("comments_total = %d, want 0", stats.CommentsTotal)
		}
	})

	t.Run("30 day window boundaries", func(t *testing.T) {
		team := s.team("Date boundaries", owner)
		s.task(team, owner, &alice, domain.StatusDone, ago(40*day), ptrTime(ago(30*day)))
		s.task(team, owner, &bob, domain.StatusDone, ago(40*day), ptrTime(ago(30*day+time.Second)))
		s.task(team, owner, &bob, domain.StatusDone, ago(day), ptrTime(now.Add(time.Second)))
		stats, err := repo.Stats(ctx, team, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(stats.TopAssignees) != 1 || stats.TopAssignees[0].UserID != alice || stats.TopAssignees[0].ClosedTasks != 1 {
			t.Fatalf("unexpected top at date boundaries: %+v", stats.TopAssignees)
		}
	})
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("TEST_MYSQL_DSN is required for integration tests")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if !safeTestDatabase(cfg.DBName) {
		t.Fatal("TEST_MYSQL_DSN must name a dedicated database ending in _test (letters, digits and underscores only)")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"

	createDatabase(t, *cfg)

	db, err := Open(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	applyMigration(t, db, "../../migrations/000001_init.down.sql")
	applyMigration(t, db, "../../migrations/000001_init.up.sql")
	return db
}

func safeTestDatabase(name string) bool {
	return len(name) <= 64 && strings.HasSuffix(name, "_test") && regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(name)
}

// createDatabase создает тестовую базу, если ее еще нет. Для этого у пользователя из DSN
// должны быть права на CREATE DATABASE (в compose тесты ходят под root).
func createDatabase(t *testing.T, cfg mysql.Config) {
	t.Helper()
	name := cfg.DBName
	cfg.DBName = ""
	db, err := Open(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatalf("connect to mysql: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE DATABASE IF NOT EXISTS `" + name + "`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
}

func applyMigration(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for _, stmt := range strings.Split(string(data), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("apply %s: %v", path, err)
		}
	}
}

type seeder struct {
	t   *testing.T
	db  *sql.DB
	now time.Time
}

func (s seeder) exec(query string, args ...any) int64 {
	s.t.Helper()
	res, err := s.db.Exec(query, args...)
	if err != nil {
		s.t.Fatalf("seed: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (s seeder) user(email, name string) int64 {
	return s.exec(`INSERT INTO users (email, password_hash, name, created_at) VALUES (?, 'x', ?, ?)`,
		email, name, s.now)
}

func (s seeder) team(name string, owner int64) int64 {
	id := s.exec(`INSERT INTO teams (name, created_by, created_at) VALUES (?, ?, ?)`, name, owner, s.now)
	s.exec(`INSERT INTO team_members (team_id, user_id, role, created_at) VALUES (?, ?, 'owner', ?)`,
		id, owner, s.now)
	return id
}

func (s seeder) task(team, author int64, assignee *int64, status domain.TaskStatus, created time.Time, closed *time.Time) int64 {
	return s.exec(`
		INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id,
		                   created_at, updated_at, closed_at, version)
		VALUES (?, 'task', '', ?, ?, ?, ?, ?, ?, 1)`,
		team, status, author, assignee, created, created, closed)
}

func (s seeder) comment(task, user int64) {
	s.exec(`INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, 'hi', ?)`,
		task, user, s.now)
}

func ptrTime(t time.Time) *time.Time { return &t }
