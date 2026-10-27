package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

// Сначала отбираем задачи команды. Все метрики дальше считаются только по ним.
const teamStatsQuery = `
WITH team_tasks AS (
    SELECT id, status, assignee_id, created_at, closed_at
    FROM tasks
    WHERE team_id = ?
),
closed_last_30d AS (
    SELECT t.assignee_id,
           u.name,
           COUNT(*) AS closed_cnt,
           ROW_NUMBER() OVER (ORDER BY COUNT(*) DESC, t.assignee_id) AS place
    FROM team_tasks t
    JOIN users u ON u.id = t.assignee_id
    WHERE t.status = 'done'
      AND t.closed_at >= ? - INTERVAL 30 DAY
      AND t.closed_at <= ?
    GROUP BY t.assignee_id, u.name
)
SELECT 'status' AS metric, status AS label, NULL AS user_id, COUNT(*) AS value
FROM team_tasks
GROUP BY status

UNION ALL

SELECT 'top_assignee', name, assignee_id, closed_cnt
FROM closed_last_30d
WHERE place <= 3

UNION ALL

SELECT 'avg_close_seconds', NULL, NULL, AVG(TIMESTAMPDIFF(SECOND, created_at, closed_at))
FROM team_tasks
WHERE closed_at IS NOT NULL

UNION ALL

SELECT 'comments', NULL, NULL, COUNT(c.id)
FROM team_tasks t
JOIN task_comments c ON c.task_id = t.id

ORDER BY metric, value DESC, user_id`

// Stats считает отчет по команде. now передается снаружи, чтобы окно в 30 дней
// можно было зафиксировать в тестах.
func (r *TeamRepo) Stats(ctx context.Context, teamID int64, now time.Time) (*domain.TeamStats, error) {
	rows, err := conn(ctx, r.db).QueryContext(ctx, teamStatsQuery, teamID, now, now)
	if err != nil {
		return nil, fmt.Errorf("select team stats: %w", err)
	}
	defer rows.Close()

	stats := &domain.TeamStats{
		TeamID:        teamID,
		TasksByStatus: make(map[domain.TaskStatus]int, len(domain.AllStatuses)),
		TopAssignees:  []domain.AssigneeStat{},
	}
	for _, s := range domain.AllStatuses {
		stats.TasksByStatus[s] = 0
	}

	for rows.Next() {
		var (
			metric string
			label  sql.NullString
			userID sql.NullInt64
			value  sql.NullFloat64
		)
		if err := rows.Scan(&metric, &label, &userID, &value); err != nil {
			return nil, fmt.Errorf("scan team stats: %w", err)
		}

		switch metric {
		case "status":
			stats.TasksByStatus[domain.TaskStatus(label.String)] = int(value.Float64)
		case "top_assignee":
			stats.TopAssignees = append(stats.TopAssignees, domain.AssigneeStat{
				UserID:      userID.Int64,
				Name:        label.String,
				ClosedTasks: int(value.Float64),
			})
		case "avg_close_seconds":
			if value.Valid {
				avg := value.Float64
				stats.AvgCloseSeconds = &avg
			}
		case "comments":
			stats.CommentsTotal = int(value.Float64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}
