package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nikitaredbee-lang/task-service/internal/auth"
	"github.com/nikitaredbee-lang/task-service/internal/cache"
	"github.com/nikitaredbee-lang/task-service/internal/config"
	"github.com/nikitaredbee-lang/task-service/internal/httpapi"
	"github.com/nikitaredbee-lang/task-service/internal/logger"
	"github.com/nikitaredbee-lang/task-service/internal/ratelimit"
	"github.com/nikitaredbee-lang/task-service/internal/repository"
	"github.com/nikitaredbee-lang/task-service/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := repository.Open(ctx, cfg.MySQL.DSN())
	if err != nil {
		return err
	}
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()
	// Redis не критичен (кеш и лимитер умеют работать без него), поэтому только предупреждаем
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn("redis is not available on start", slog.Any("error", err))
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTL)
	txm := repository.NewTxManager(db)
	users := repository.NewUserRepo(db)
	teams := repository.NewTeamRepo(db)
	tasks := repository.NewTaskRepo(db)
	taskCache := cache.NewTaskListCache(rdb, cfg.TasksCacheTTL)

	var limiter *ratelimit.Limiter
	if cfg.RateLimitPerMinute > 0 {
		limiter = ratelimit.New(rdb, cfg.RateLimitPerMinute, time.Minute)
	}

	router := httpapi.NewRouter(httpapi.Deps{
		Auth:    service.NewAuthService(users, tokens),
		Teams:   service.NewTeamService(txm, teams, users),
		Tasks:   service.NewTaskService(txm, tasks, teams, taskCache, log),
		Tokens:  tokens,
		Limiter: limiter,
		Log:     log,
		// Redis в health не проверяем: без него сервис работает, просто медленнее
		// и без лимитов. А вот без MySQL работать нечем.
		Health: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			return db.PingContext(ctx)
		},
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Info("http server started", slog.String("addr", ln.Addr().String()))

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
