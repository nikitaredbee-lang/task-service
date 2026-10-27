package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Config struct {
	HTTPAddr string
	LogLevel string

	MySQL MySQL
	Redis Redis

	JWTSecret string
	JWTTTL    time.Duration

	TasksCacheTTL time.Duration
	// 0 выключает ограничение частоты запросов
	RateLimitPerMinute int
}

type MySQL struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

func (m MySQL) DSN() string {
	c := mysql.NewConfig()
	c.Net = "tcp"
	c.Addr = net.JoinHostPort(m.Host, m.Port)
	c.User = m.User
	c.Passwd = m.Password
	c.DBName = m.Database
	c.ParseTime = true
	c.Loc = time.UTC
	// чтобы NOW() и прочее в базе тоже работало в UTC
	c.Params = map[string]string{"time_zone": "'+00:00'"}
	return c.FormatDSN()
}

type Redis struct {
	Addr     string
	Password string
	DB       int
}

// Load читает конфиг из переменных окружения. Для локального запуска у большинства
// параметров есть дефолты, секреты обязательны.
func Load() (*Config, error) {
	var errs []error

	cfg := &Config{
		HTTPAddr: getEnv("HTTP_ADDR", ":8080"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
		MySQL: MySQL{
			Host:     getEnv("MYSQL_HOST", "localhost"),
			Port:     getEnv("MYSQL_PORT", "3306"),
			User:     getEnv("MYSQL_USER", "tasks"),
			Password: os.Getenv("MYSQL_PASSWORD"),
			Database: getEnv("MYSQL_DATABASE", "tasks"),
		},
		Redis: Redis{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
		},
		JWTSecret: os.Getenv("JWT_SECRET"),
	}

	if cfg.MySQL.Password == "" {
		errs = append(errs, errors.New("MYSQL_PASSWORD is required"))
	}
	if len(cfg.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET is required and must be at least 32 characters"))
	}

	var err error
	if cfg.Redis.DB, err = getInt("REDIS_DB", 0); err != nil {
		errs = append(errs, err)
	}
	if cfg.JWTTTL, err = getDuration("JWT_TTL", 24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.TasksCacheTTL, err = getDuration("TASKS_CACHE_TTL", 5*time.Minute); err != nil {
		errs = append(errs, err)
	}
	if cfg.RateLimitPerMinute, err = getInt("RATE_LIMIT_PER_MINUTE", 120); err != nil {
		errs = append(errs, err)
	}
	if cfg.Redis.DB < 0 {
		errs = append(errs, errors.New("REDIS_DB must not be negative"))
	}
	if cfg.JWTTTL < time.Second {
		errs = append(errs, errors.New("JWT_TTL must be at least 1s"))
	}
	if cfg.TasksCacheTTL <= 0 {
		errs = append(errs, errors.New("TASKS_CACHE_TTL must be positive"))
	}
	if cfg.RateLimitPerMinute < 0 {
		errs = append(errs, errors.New("RATE_LIMIT_PER_MINUTE must not be negative"))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration like 5m or 24h: %w", key, err)
	}
	return d, nil
}
