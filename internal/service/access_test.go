package service

import (
	"errors"
	"testing"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

func ptr[T any](v T) *T { return &v }

func TestCheckTaskEdit(t *testing.T) {
	const (
		author   int64 = 1
		assignee int64 = 2
		stranger int64 = 3
	)
	task := &domain.Task{ID: 10, TeamID: 1, CreatedBy: author, AssigneeID: ptr(assignee)}

	tests := []struct {
		name    string
		role    domain.Role
		userID  int64
		fields  []string
		allowed bool
	}{
		{"owner edits any field", domain.RoleOwner, stranger, []string{fieldTitle, fieldAssignee}, true},
		{"admin edits any field", domain.RoleAdmin, stranger, []string{fieldDescription, fieldStatus}, true},
		{"author edits title and description", domain.RoleMember, author, []string{fieldTitle, fieldDescription}, true},
		{"author reassigns task", domain.RoleMember, author, []string{fieldAssignee}, true},
		{"author changes status", domain.RoleMember, author, []string{fieldStatus}, true},
		{"assignee changes status", domain.RoleMember, assignee, []string{fieldStatus}, true},
		{"assignee can not reassign", domain.RoleMember, assignee, []string{fieldAssignee}, false},
		{"assignee can not rename", domain.RoleMember, assignee, []string{fieldStatus, fieldTitle}, false},
		{"stranger can not change status", domain.RoleMember, stranger, []string{fieldStatus}, false},
		{"stranger can not rename", domain.RoleMember, stranger, []string{fieldTitle}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTaskEdit(tt.role, tt.userID, task, tt.fields)
			assertAllowed(t, err, tt.allowed)
		})
	}
}

func TestCheckTaskEdit_UnassignedTask(t *testing.T) {
	task := &domain.Task{ID: 10, TeamID: 1, CreatedBy: 1}
	err := checkTaskEdit(domain.RoleMember, 2, task, []string{fieldStatus})
	assertAllowed(t, err, false)
}

func TestCheckInvite(t *testing.T) {
	tests := []struct {
		name    string
		actor   domain.Role
		newRole domain.Role
		allowed bool
	}{
		{"owner invites member", domain.RoleOwner, domain.RoleMember, true},
		{"owner invites admin", domain.RoleOwner, domain.RoleAdmin, true},
		{"owner can not invite owner", domain.RoleOwner, domain.RoleOwner, false},
		{"admin invites member", domain.RoleAdmin, domain.RoleMember, true},
		{"admin can not invite admin", domain.RoleAdmin, domain.RoleAdmin, false},
		{"admin can not invite owner", domain.RoleAdmin, domain.RoleOwner, false},
		{"member can not invite", domain.RoleMember, domain.RoleMember, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAllowed(t, checkInvite(tt.actor, tt.newRole), tt.allowed)
		})
	}
}

func TestCheckRoleChange(t *testing.T) {
	tests := []struct {
		name    string
		actor   domain.Role
		target  domain.Role
		newRole domain.Role
		allowed bool
	}{
		{"owner promotes member to admin", domain.RoleOwner, domain.RoleMember, domain.RoleAdmin, true},
		{"owner demotes admin", domain.RoleOwner, domain.RoleAdmin, domain.RoleMember, true},
		{"owner can not change own role", domain.RoleOwner, domain.RoleOwner, domain.RoleMember, false},
		{"owner can not make another owner", domain.RoleOwner, domain.RoleAdmin, domain.RoleOwner, false},
		{"admin can not change roles", domain.RoleAdmin, domain.RoleMember, domain.RoleAdmin, false},
		{"admin can not demote owner", domain.RoleAdmin, domain.RoleOwner, domain.RoleMember, false},
		{"member can not change roles", domain.RoleMember, domain.RoleMember, domain.RoleAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAllowed(t, checkRoleChange(tt.actor, tt.target, tt.newRole), tt.allowed)
		})
	}
}

func TestCanViewStats(t *testing.T) {
	cases := map[domain.Role]bool{
		domain.RoleOwner:  true,
		domain.RoleAdmin:  true,
		domain.RoleMember: false,
	}
	for role, want := range cases {
		if got := canViewStats(role); got != want {
			t.Errorf("canViewStats(%s) = %v, want %v", role, got, want)
		}
	}
}

func TestApplyChanges(t *testing.T) {
	cur := &domain.Task{
		ID: 1, Title: "old", Description: "d", Status: domain.StatusInProgress,
		AssigneeID: ptr[int64](5), Version: 3,
	}

	t.Run("same values are not changes", func(t *testing.T) {
		next, changes := applyChanges(cur, UpdateTaskInput{
			Title:       ptr("old"),
			Status:      ptr(domain.StatusInProgress),
			AssigneeSet: true,
			AssigneeID:  ptr[int64](5),
		}, now())
		if len(changes) != 0 {
			t.Fatalf("expected no changes, got %v", changes)
		}
		if next.Version != 3 {
			t.Fatalf("version must stay 3, got %d", next.Version)
		}
	})

	t.Run("closing task sets closed_at", func(t *testing.T) {
		at := now()
		next, changes := applyChanges(cur, UpdateTaskInput{Status: ptr(domain.StatusDone)}, at)
		if next.ClosedAt == nil || !next.ClosedAt.Equal(at) {
			t.Fatalf("closed_at must be set to %v, got %v", at, next.ClosedAt)
		}
		if _, ok := changes[fieldClosedAt]; !ok {
			t.Fatal("closed_at change must be in history")
		}
		if next.Version != 4 {
			t.Fatalf("version must be 4, got %d", next.Version)
		}
		if got := editedFields(changes); len(got) != 1 || got[0] != fieldStatus {
			t.Fatalf("only status must be checked for access, got %v", got)
		}
	})

	t.Run("reopening task clears closed_at", func(t *testing.T) {
		closed := *cur
		closed.Status = domain.StatusDone
		closed.ClosedAt = ptr(now())
		next, _ := applyChanges(&closed, UpdateTaskInput{Status: ptr(domain.StatusTodo)}, now())
		if next.ClosedAt != nil {
			t.Fatalf("closed_at must be cleared, got %v", next.ClosedAt)
		}
	})

	t.Run("null assignee unassigns task", func(t *testing.T) {
		next, changes := applyChanges(cur, UpdateTaskInput{AssigneeSet: true, AssigneeID: nil}, now())
		if next.AssigneeID != nil {
			t.Fatalf("assignee must be cleared, got %v", *next.AssigneeID)
		}
		if _, ok := changes[fieldAssignee]; !ok {
			t.Fatal("assignee change must be in history")
		}
	})
}

func assertAllowed(t *testing.T, err error, allowed bool) {
	t.Helper()
	if allowed && err != nil {
		t.Fatalf("expected access, got error: %v", err)
	}
	if !allowed && !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden, got: %v", err)
	}
}
