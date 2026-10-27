package service

import "github.com/nikitaredbee-lang/task-service/internal/domain"

const (
	fieldTitle       = "title"
	fieldDescription = "description"
	fieldStatus      = "status"
	fieldAssignee    = "assignee_id"
	fieldClosedAt    = "closed_at"
)

func isManager(role domain.Role) bool {
	return role == domain.RoleOwner || role == domain.RoleAdmin
}

func canViewStats(role domain.Role) bool {
	return isManager(role)
}

// через приглашение нельзя обойти смену ролей (роли меняет только owner)
func checkInvite(actor, newRole domain.Role) error {
	if newRole == domain.RoleOwner {
		return domain.Forbidden("owner role can not be granted by invite")
	}
	switch actor {
	case domain.RoleOwner:
		return nil
	case domain.RoleAdmin:
		if newRole != domain.RoleMember {
			return domain.Forbidden("admin can invite only members")
		}
		return nil
	default:
		return domain.Forbidden("only team owner or admin can invite users")
	}
}

func checkRoleChange(actor, target, newRole domain.Role) error {
	if actor != domain.RoleOwner {
		return domain.Forbidden("only team owner can change roles")
	}
	if target == domain.RoleOwner {
		return domain.Forbidden("owner role can not be changed")
	}
	if newRole == domain.RoleOwner {
		return domain.Forbidden("owner role can not be granted")
	}
	return nil
}

func checkTaskEdit(role domain.Role, userID int64, task *domain.Task, fields []string) error {
	if isManager(role) {
		return nil
	}
	if task.CreatedBy == userID {
		return nil
	}
	if task.AssigneeID != nil && *task.AssigneeID == userID {
		for _, f := range fields {
			if f != fieldStatus {
				return domain.Forbidden("assignee can change only the task status")
			}
		}
		return nil
	}
	return domain.Forbidden("you can edit only tasks you created or are assigned to")
}
