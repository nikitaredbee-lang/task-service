package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Fixed window с общим счетчиком в Redis. На границе окон возможен всплеск до 2N.
type Limiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func New(rdb *redis.Client, limit int, window time.Duration) *Limiter {
	return &Limiter{rdb: rdb, limit: limit, window: window}
}

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// TODO: если понадобится точнее, переделать на sliding window
func (l *Limiter) Allow(ctx context.Context, key string) (Result, error) {
	now := time.Now()
	windowStart := now.Truncate(l.window)
	redisKey := fmt.Sprintf("ratelimit:%s:%d", key, windowStart.Unix())

	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return Result{}, fmt.Errorf("rate limit incr: %w", err)
	}

	count := int(incr.Val())
	res := Result{
		Allowed:   count <= l.limit,
		Limit:     l.limit,
		Remaining: max(l.limit-count, 0),
	}
	if !res.Allowed {
		res.RetryAfter = windowStart.Add(l.window).Sub(now)
	}
	return res, nil
}
