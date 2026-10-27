package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

type TaskRepo struct {
	db *sql.DB
}

func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

const taskColumns = `id, team_id, title, description, status, created_by, assignee_id,
	created_at, updated_at, closed_at, version`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(s rowScanner) (*domain.Task, error) {
	var (
		t        domain.Task
		assignee sql.NullInt64
		closedAt sql.NullTime
	)
	err := s.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status, &t.CreatedBy, &assignee,
		&t.CreatedAt, &t.UpdatedAt, &closedAt, &t.Version)
	if err != nil {
		return nil, err
	}
	if assignee.Valid {
		t.AssigneeID = &assignee.Int64
	}
	if closedAt.Valid {
		t.ClosedAt = &closedAt.Time
	}
	return &t, nil
}

func (r *TaskRepo) Create(ctx context.Context, t *domain.Task) error {
	res, err := conn(ctx, r.db).ExecContext(ctx, `
		INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id,
		                   created_at, updated_at, closed_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TeamID, t.Title, t.Description, t.Status, t.CreatedBy, t.AssigneeID,
		t.CreatedAt, t.UpdatedAt, t.ClosedAt, t.Version)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (r *TaskRepo) Get(ctx context.Context, id int64) (*domain.Task, error) {
	return r.get(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
}

// GetForUpdate читает задачу и блокирует строку до конца транзакции.
// Вызывать только внутри TxManager.Do, иначе блокировка сразу отпустится.
func (r *TaskRepo) GetForUpdate(ctx context.Context, id int64) (*domain.Task, error) {
	return r.get(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ? FOR UPDATE`, id)
}

func (r *TaskRepo) get(ctx context.Context, query string, id int64) (*domain.Task, error) {
	t, err := scanTask(conn(ctx, r.db).QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select task: %w", err)
	}
	return t, nil
}

// Условие по версии страхует от записи поверх чужого изменения.
func (r *TaskRepo) Update(ctx context.Context, t *domain.Task, prevVersion int) error {
	res, err := conn(ctx, r.db).ExecContext(ctx, `
		UPDATE tasks
		SET title = ?, description = ?, status = ?, assignee_id = ?,
		    updated_at = ?, closed_at = ?, version = ?
		WHERE id = ? AND version = ?`,
		t.Title, t.Description, t.Status, t.AssigneeID,
		t.UpdatedAt, t.ClosedAt, t.Version,
		t.ID, prevVersion)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	if n == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *TaskRepo) List(ctx context.Context, f domain.TaskFilter) ([]domain.Task, int, error) {
	var (
		where strings.Builder
		args  []any
	)
	where.WriteString("team_id = ?")
	args = append(args, f.TeamID)
	if f.Status != "" {
		where.WriteString(" AND status = ?")
		args = append(args, f.Status)
	}
	if f.AssigneeID != nil {
		where.WriteString(" AND assignee_id = ?")
		args = append(args, *f.AssigneeID)
	}

	db := conn(ctx, r.db)

	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE `+where.String(), args...).
		Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	query := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + where.String() +
		` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := db.QueryContext(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("select tasks: %w", err)
	}
	defer rows.Close()

	tasks := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}
