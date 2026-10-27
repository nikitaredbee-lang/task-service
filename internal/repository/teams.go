package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

type TeamRepo struct {
	db *sql.DB
}

func NewTeamRepo(db *sql.DB) *TeamRepo {
	return &TeamRepo{db: db}
}

func (r *TeamRepo) Create(ctx context.Context, t *domain.Team) error {
	res, err := conn(ctx, r.db).ExecContext(ctx,
		`INSERT INTO teams (name, created_by, created_at) VALUES (?, ?, ?)`,
		t.Name, t.CreatedBy, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert team: %w", err)
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (r *TeamRepo) AddMember(ctx context.Context, teamID, userID int64, role domain.Role, at time.Time) error {
	_, err := conn(ctx, r.db).ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		teamID, userID, role, at)
	if err != nil {
		if isDuplicate(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("insert team member: %w", err)
	}
	return nil
}

// MemberRole возвращает роль пользователя в команде или ErrNotFound, если он не участник.
func (r *TeamRepo) MemberRole(ctx context.Context, teamID, userID int64) (domain.Role, error) {
	var role domain.Role
	err := conn(ctx, r.db).QueryRowContext(ctx,
		`SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).
		Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("select member role: %w", err)
	}
	return role, nil
}

func (r *TeamRepo) UpdateMemberRole(ctx context.Context, teamID, userID int64, role domain.Role) error {
	_, err := conn(ctx, r.db).ExecContext(ctx,
		`UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?`, role, teamID, userID)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	return nil
}

func (r *TeamRepo) ListByUser(ctx context.Context, userID int64) ([]domain.Team, error) {
	rows, err := conn(ctx, r.db).QueryContext(ctx, `
		SELECT t.id, t.name, t.created_by, t.created_at, m.role
		FROM teams t
		JOIN team_members m ON m.team_id = t.id
		WHERE m.user_id = ?
		ORDER BY t.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("select teams: %w", err)
	}
	defer rows.Close()

	teams := []domain.Team{}
	for rows.Next() {
		var t domain.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt, &t.Role); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}
