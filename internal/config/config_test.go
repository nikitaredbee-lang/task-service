package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsInvalidLimits(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"JWT_TTL", "0s"},
		{"JWT_TTL", "500ms"},
		{"TASKS_CACHE_TTL", "-1m"},
		{"TASKS_CACHE_TTL", "0s"},
		{"REDIS_DB", "-1"},
		{"RATE_LIMIT_PER_MINUTE", "-1"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("MYSQL_PASSWORD", "test")
			t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
			for _, key := range []string{"JWT_TTL", "TASKS_CACHE_TTL", "REDIS_DB", "RATE_LIMIT_PER_MINUTE"} {
				t.Setenv(key, "")
			}
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected validation error for %s, got %v", tc.key, err)
			}
		})
	}
}

func TestLoadAllowsDisabledRateLimit(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "test")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("RATE_LIMIT_PER_MINUTE", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RateLimitPerMinute != 0 {
		t.Fatal("rate limit should be disabled")
	}
}
