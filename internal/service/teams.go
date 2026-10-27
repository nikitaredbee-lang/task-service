package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

type TeamService struct {
	tx    TxManager
	teams TeamRepository
	users UserRepository
}

func NewTeamService(tx TxManager, teams TeamRepository, users UserRepository) *TeamService {
	return &TeamService{tx: tx, teams: teams, users: users}
}

func (s *TeamService) Create(ctx context.Context, userID int64, name string) (*domain.Team, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, domain.Invalid("name must be from 1 to 100 characters")
	}

	created := now()
	team := &domain.Team{
		Name:      name,
		CreatedBy: userID,
		CreatedAt: created,
		Role:      domain.RoleOwner,
	}
	// владельца добавляем в той же транзакции что и команду
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.teams.Create(ctx, team); err != nil {
			return err
		}
		return s.teams.AddMember(ctx, team.ID, userID, domain.RoleOwner, created)
	})
	if err != nil {
		return nil, err
	}
	return team, nil
}

func (s *TeamService) List(ctx context.Context, userID int64) ([]domain.Team, error) {
	return s.teams.ListByUser(ctx, userID)
}

type InviteInput struct {
	Email string
	Role  domain.Role
}

func (s *TeamService) Invite(ctx context.Context, actorID, teamID int64, in InviteInput) (*domain.Member, error) {
	if in.Role == "" {
		in.Role = domain.RoleMember
	}
	if !in.Role.Valid() {
		return nil, domain.Invalid("role must be admin or member")
	}

	var member *domain.Member
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		actorRole, err := s.memberRole(ctx, teamID, actorID)
		if err != nil {
			return err
		}
		if err := checkInvite(actorRole, in.Role); err != nil {
			return err
		}

		user, err := s.users.GetByEmail(ctx, normalizeEmail(in.Email))
		if errors.Is(err, domain.ErrNotFound) {
			return domain.NotFound("user with this email is not registered")
		}
		if err != nil {
			return err
		}

		err = s.teams.AddMember(ctx, teamID, user.ID, in.Role, now())
		if errors.Is(err, domain.ErrConflict) {
			return domain.Conflict("user is already a member of the team")
		}
		if err != nil {
			return err
		}

		member = &domain.Member{
			TeamID: teamID,
			UserID: user.ID,
			Email:  user.Email,
			Name:   user.Name,
			Role:   in.Role,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

func (s *TeamService) ChangeRole(ctx context.Context, actorID, teamID, targetID int64, role domain.Role) error {
	if !role.Valid() {
		return domain.Invalid("role must be admin or member")
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		actorRole, err := s.memberRole(ctx, teamID, actorID)
		if err != nil {
			return err
		}
		targetRole, err := s.teams.MemberRole(ctx, teamID, targetID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.NotFound("user is not a member of the team")
		}
		if err != nil {
			return err
		}
		if err := checkRoleChange(actorRole, targetRole, role); err != nil {
			return err
		}
		return s.teams.UpdateMemberRole(ctx, teamID, targetID, role)
	})
}

func (s *TeamService) Stats(ctx context.Context, userID, teamID int64) (*domain.TeamStats, error) {
	role, err := s.memberRole(ctx, teamID, userID)
	if err != nil {
		return nil, err
	}
	if !canViewStats(role) {
		return nil, domain.Forbidden("only team owner or admin can view stats")
	}
	return s.teams.Stats(ctx, teamID, now())
}

// memberRole возвращает роль пользователя в команде. Если он в команде не состоит,
// отвечаем как будто команды нет, чтобы нельзя было перебором узнать чужие id.
func (s *TeamService) memberRole(ctx context.Context, teamID, userID int64) (domain.Role, error) {
	role, err := s.teams.MemberRole(ctx, teamID, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.NotFound("team not found")
	}
	return role, err
}
