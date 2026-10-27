package service

import (
	"context"
	"time"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type UserRepository interface {
	Create(ctx context.Context, u *domain.User) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type TeamRepository interface {
	Create(ctx context.Context, t *domain.Team) error
	AddMember(ctx context.Context, teamID, userID int64, role domain.Role, at time.Time) error
	MemberRole(ctx context.Context, teamID, userID int64) (domain.Role, error)
	UpdateMemberRole(ctx context.Context, teamID, userID int64, role domain.Role) error
	ListByUser(ctx context.Context, userID int64) ([]domain.Team, error)
	Stats(ctx context.Context, teamID int64, now time.Time) (*domain.TeamStats, error)
}

type TaskRepository interface {
	Create(ctx context.Context, t *domain.Task) error
	Get(ctx context.Context, id int64) (*domain.Task, error)
	GetForUpdate(ctx context.Context, id int64) (*domain.Task, error)
	Update(ctx context.Context, t *domain.Task, prevVersion int) error
	List(ctx context.Context, f domain.TaskFilter) ([]domain.Task, int, error)
	AddHistory(ctx context.Context, h *domain.HistoryEntry) error
	History(ctx context.Context, taskID int64) ([]domain.HistoryEntry, error)
	AddComment(ctx context.Context, c *domain.Comment) error
	Comments(ctx context.Context, taskID int64, limit, offset int) ([]domain.Comment, error)
}

// Set использует поколение из Get, чтобы запоздавшее чтение не вернуло старую страницу в кеш.
type TaskListCache interface {
	Get(ctx context.Context, f domain.TaskFilter) (page *domain.TaskPage, gen string, err error)
	Set(ctx context.Context, f domain.TaskFilter, gen string, page *domain.TaskPage) error
	Invalidate(ctx context.Context, teamID int64) error
}

type TokenIssuer interface {
	Issue(userID int64) (string, error)
}

func now() time.Time {
	// в базе DATETIME(3), поэтому сразу режем до миллисекунд,
	// иначе в ответе и в базе будут разные значения
	return time.Now().UTC().Truncate(time.Millisecond)
}
