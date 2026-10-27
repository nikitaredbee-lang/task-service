package cache

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

// Одно поколение на команду: смена токена делает все ее страницы недоступными.
type TaskListCache struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewTaskListCache(rdb *redis.Client, ttl time.Duration) *TaskListCache {
	return &TaskListCache{rdb: rdb, ttl: ttl}
}

func (c *TaskListCache) Get(ctx context.Context, f domain.TaskFilter) (*domain.TaskPage, string, error) {
	key := genKey(f.TeamID)
	gen, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		// После eviction нельзя начинать счетчик с нуля: страницы с таким номером
		// могут еще жить. Новый случайный токен не переиспользует старые ключи.
		if err := c.rdb.SetNX(ctx, key, rand.Text(), 0).Err(); err != nil {
			return nil, "", fmt.Errorf("init cache generation: %w", err)
		}
		gen, err = c.rdb.Get(ctx, key).Result()
	}
	if err != nil {
		return nil, "", fmt.Errorf("get cache generation: %w", err)
	}

	data, err := c.rdb.Get(ctx, listKey(f, gen)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, gen, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("get task list from cache: %w", err)
	}

	var page domain.TaskPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, "", fmt.Errorf("decode cached task list: %w", err)
	}
	return &page, gen, nil
}

func (c *TaskListCache) Set(ctx context.Context, f domain.TaskFilter, gen string, page *domain.TaskPage) error {
	data, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("encode task list: %w", err)
	}
	return c.rdb.Set(ctx, listKey(f, gen), data, c.ttl).Err()
}

func (c *TaskListCache) Invalidate(ctx context.Context, teamID int64) error {
	// TODO: ретрай если redis моргнул, пока только пишем в лог выше по стеку
	return c.rdb.Set(ctx, genKey(teamID), rand.Text(), 0).Err()
}

func genKey(teamID int64) string {
	return fmt.Sprintf("tasks:team:%d:gen", teamID)
}

func listKey(f domain.TaskFilter, gen string) string {
	status := string(f.Status)
	if status == "" {
		status = "any"
	}
	assignee := "any"
	if f.AssigneeID != nil {
		assignee = strconv.FormatInt(*f.AssigneeID, 10)
	}
	return fmt.Sprintf("tasks:team:%d:gen:%s:list:status=%s:assignee=%s:limit=%d:offset=%d",
		f.TeamID, gen, status, assignee, f.Limit, f.Offset)
}
