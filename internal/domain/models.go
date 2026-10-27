package domain

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember:
		return true
	}
	return false
}

type TaskStatus string

const (
	StatusTodo       TaskStatus = "todo"
	StatusInProgress TaskStatus = "in_progress"
	StatusDone       TaskStatus = "done"
)

var AllStatuses = []TaskStatus{StatusTodo, StatusInProgress, StatusDone}

func (s TaskStatus) Valid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusDone:
		return true
	}
	return false
}

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	// Роль текущего пользователя в команде, заполняется в списке команд.
	Role Role `json:"role,omitempty"`
}

type Member struct {
	TeamID int64  `json:"team_id"`
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
}

type Task struct {
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	CreatedBy   int64      `json:"created_by"`
	AssigneeID  *int64     `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Version     int        `json:"version"`
}

type TaskFilter struct {
	TeamID     int64
	Status     TaskStatus // пустая строка значит без фильтра
	AssigneeID *int64
	Limit      int
	Offset     int
}

type TaskPage struct {
	Items  []Task `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type FieldChange struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type HistoryEntry struct {
	ID        int64                  `json:"id"`
	TaskID    int64                  `json:"task_id"`
	ChangedBy int64                  `json:"changed_by"`
	Changes   map[string]FieldChange `json:"changes"`
	CreatedAt time.Time              `json:"created_at"`
}

type Comment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type AssigneeStat struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	ClosedTasks int    `json:"closed_tasks"`
}

type TeamStats struct {
	TeamID        int64              `json:"team_id"`
	TasksByStatus map[TaskStatus]int `json:"tasks_by_status"`
	TopAssignees  []AssigneeStat     `json:"top_assignees_30d"`
	// Среднее время от создания до закрытия по всем закрытым задачам в секундах.
	// null если закрытых задач нет.
	AvgCloseSeconds *float64 `json:"avg_close_seconds"`
	CommentsTotal   int      `json:"comments_total"`
}
