//go:build integration

package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nikitaredbee-lang/task-service/internal/domain"
)

func TestTaskListCache(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Fatal("TEST_REDIS_ADDR is required for integration tests")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("TEST_REDIS_PASSWORD")})
	t.Cleanup(func() { rdb.Close() })
	ctx := context.Background()
	teamID := time.Now().UnixNano()
	f := domain.TaskFilter{TeamID: teamID, Limit: 20}
	c := NewTaskListCache(rdb, 5*time.Minute)
	page := &domain.TaskPage{Items: []domain.Task{{ID: 1}}, Total: 1, Limit: 20}
	assigneeID := int64(7)
	var keys []string
	t.Cleanup(func() { rdb.Del(ctx, append(keys, genKey(teamID))...) })

	miss, oldGen, err := c.Get(ctx, f)
	if err != nil || miss != nil || oldGen == "" {
		t.Fatalf("initial lookup: page=%+v, gen=%q, err=%v", miss, oldGen, err)
	}
	if err := c.Set(ctx, f, oldGen, page); err != nil {
		t.Fatal(err)
	}
	keys = append(keys, listKey(f, oldGen))
	hit, _, err := c.Get(ctx, f)
	if err != nil || hit == nil || hit.Total != 1 {
		t.Fatalf("cache hit: page=%+v, err=%v", hit, err)
	}
	if ttl := rdb.TTL(ctx, listKey(f, oldGen)).Val(); ttl <= 0 || ttl > 5*time.Minute {
		t.Fatalf("unexpected TTL: %v", ttl)
	}
	for name, other := range map[string]domain.TaskFilter{
		"status":   {TeamID: teamID, Limit: 20, Status: domain.StatusDone},
		"assignee": {TeamID: teamID, Limit: 20, AssigneeID: &assigneeID},
		"limit":    {TeamID: teamID, Limit: 10},
		"offset":   {TeamID: teamID, Limit: 20, Offset: 20},
		"team":     {TeamID: teamID + 1, Limit: 20},
	} {
		p, _, err := c.Get(ctx, other)
		keys = append(keys, genKey(other.TeamID))
		if err != nil || p != nil {
			t.Fatalf("%s must have a separate key: page=%+v, err=%v", name, p, err)
		}
	}
	if err := c.Invalidate(ctx, teamID); err != nil {
		t.Fatal(err)
	}
	// Чтение могло начаться до коммита и закончиться уже после инвалидации.
	if err := c.Set(ctx, f, oldGen, page); err != nil {
		t.Fatal(err)
	}
	p, gen, err := c.Get(ctx, f)
	if err != nil || p != nil || gen == oldGen {
		t.Fatalf("late write reused: page=%+v, gen=%q, err=%v", p, gen, err)
	}
	if err := c.Set(ctx, f, gen, page); err != nil {
		t.Fatal(err)
	}
	keys = append(keys, listKey(f, gen))
	// Имитируем вытеснение одного ключа поколения. Страницы при этом еще живы.
	if err := rdb.Del(ctx, genKey(teamID)).Err(); err != nil {
		t.Fatal(err)
	}
	p, restoredGen, err := c.Get(ctx, f)
	if err != nil || p != nil || restoredGen == gen || restoredGen == oldGen {
		t.Fatalf("eviction reused a generation: page=%+v, gen=%q, err=%v", p, restoredGen, err)
	}
}
