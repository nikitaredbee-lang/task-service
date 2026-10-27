package repository

import (
	"context"
	"fmt"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

func (r *TaskRepo) AddComment(ctx context.Context, c *domain.Comment) error {
	res, err := conn(ctx, r.db).ExecContext(ctx,
		`INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, ?, ?)`,
		c.TaskID, c.UserID, c.Content, c.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert comment: %w", err)
	}
	c.ID, err = res.LastInsertId()
	return err
}

func (r *TaskRepo) Comments(ctx context.Context, taskID int64, limit, offset int) ([]domain.Comment, error) {
	rows, err := conn(ctx, r.db).QueryContext(ctx, `
		SELECT id, task_id, user_id, content, created_at
		FROM task_comments
		WHERE task_id = ?
		ORDER BY id
		LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("select comments: %w", err)
	}
	defer rows.Close()

	comments := []domain.Comment{}
	for rows.Next() {
		var c domain.Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
