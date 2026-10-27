-include .env
export

MYSQL_EXPOSE_PORT ?= 3306
MIGRATE_DSN = mysql://$(MYSQL_USER):$(MYSQL_PASSWORD)@tcp(mysql:3306)/$(MYSQL_DATABASE)?multiStatements=true

.PHONY: up down logs test test-integration lint migrate-up migrate-down

up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f app

test:
	go test ./...

test-integration:
	docker compose up -d --wait mysql redis
	TEST_MYSQL_DSN='root:$(MYSQL_ROOT_PASSWORD)@tcp(localhost:$(MYSQL_EXPOSE_PORT))/$(MYSQL_DATABASE)_test' \
		TEST_REDIS_ADDR='localhost:$(or $(REDIS_EXPOSE_PORT),6379)' \
		TEST_REDIS_PASSWORD='$(REDIS_PASSWORD)' \
		go test -tags=integration -count=1 ./...

lint:
	go vet ./...

migrate-up:
	docker compose run --rm migrate

migrate-down:
	docker compose run --rm migrate -path=/migrations -database='$(MIGRATE_DSN)' down 1
