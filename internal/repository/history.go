package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

func (r *TaskRepo) AddHistory(ctx context.Context, h *domain.HistoryEntry) error {
	changes, err := json.Marshal(h.Changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}
	res, err := conn(ctx, r.db).ExecContext(ctx,
		`INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES (?, ?, ?, ?)`,
		h.TaskID, h.ChangedBy, changes, h.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert task history: %w", err)
	}
	h.ID, err = res.LastInsertId()
	return err
}

func (r *TaskRepo) History(ctx context.Context, taskID int64) ([]domain.HistoryEntry, error) {
	rows, err := conn(ctx, r.db).QueryContext(ctx, `
		SELECT id, task_id, changed_by, changes, created_at
		FROM task_history
		WHERE task_id = ?
		ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("select task history: %w", err)
	}
	defer rows.Close()

	entries := []domain.HistoryEntry{}
	for rows.Next() {
		var (
			h   domain.HistoryEntry
			raw []byte
		)
		if err := rows.Scan(&h.ID, &h.TaskID, &h.ChangedBy, &raw, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan task history: %w", err)
		}
		// UseNumber чтобы id не превращались во float64
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&h.Changes); err != nil {
			return nil, fmt.Errorf("decode history changes: %w", err)
		}
		entries = append(entries, h)
	}
	return entries, rows.Err()
}
