# Task Service

REST API для задач внутри команд. Go 1.24.7+, MySQL 8.4, Redis 7, chi. Схема API лежит в [api/openapi.yaml](api/openapi.yaml).

## Запуск

Нужны Docker и Docker Compose:

```bash
cp .env.example .env
docker compose up --build -d
docker compose logs -f app
```

Compose дождётся MySQL и Redis, применит миграции и запустит API.

- API: http://localhost:8080/api/v1
- Swagger UI: http://localhost:8080/swagger/
- Проверка доступности MySQL: http://localhost:8080/health

Swagger грузит JS и CSS с CDN и без интернета не откроется. Сама OpenAPI-схема встроена в бинарник и доступна по `/swagger/openapi.yaml`.

Настройки берутся из переменных окружения. Пример лежит в `.env.example`. Для запуска без Docker нужно экспортировать эти переменные и выполнить `go run ./cmd/api`. Приложение само файл `.env` не читает.

| Настройка | По умолчанию |
| --- | --- |
| `HTTP_ADDR`, `LOG_LEVEL` | `:8080`, `info` |
| `MYSQL_HOST`, `MYSQL_PORT` | `localhost`, `3306` |
| `MYSQL_DATABASE`, `MYSQL_USER` | `tasks`, `tasks` |
| `MYSQL_PASSWORD` | обязательна |
| `REDIS_ADDR` | `localhost:6379` |
| `REDIS_PASSWORD`, `REDIS_DB` | пустой пароль, `0` |
| `JWT_SECRET` | обязателен, минимум 32 байта |
| `JWT_TTL` | `24h`, минимум `1s` |
| `TASKS_CACHE_TTL` | `5m`, должен быть положительным |
| `RATE_LIMIT_PER_MINUTE` | `120`, `0` отключает лимит |

`HTTP_PORT`, `MYSQL_EXPOSE_PORT` и `REDIS_EXPOSE_PORT` меняют опубликованные порты Compose. MYSQL_ROOT_PASSWORD нужен контейнеру MySQL и интеграционным тестам. Пароли из примера предназначены для локального запуска.

Миграции лежат в `migrations/`, формат golang-migrate. Compose применяет их автоматически. Для ручного запуска:

```bash
make migrate-up
make migrate-down  # откат одной миграции
```

## Пример работы

Для примера нужен `jq`. Почты пользователей должны быть свободны. ID берутся из ответов, так что пример не зависит от нумерации в базе.

```bash
API=http://localhost:8080/api/v1

curl -sS "$API/register" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123","name":"Alice"}'

BOB_ID=$(curl -sS "$API/register" -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","password":"password123","name":"Bob"}' | jq -er .id)

TOKEN=$(curl -sS "$API/login" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}' | jq -er .access_token)

TEAM_ID=$(curl -sS "$API/teams" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Payments"}' | jq -er .id)

curl -sS "$API/teams/$TEAM_ID/invite" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"bob@example.com","role":"member"}'

TASK=$(curl -sS "$API/tasks" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --argjson team "$TEAM_ID" --argjson bob "$BOB_ID" \
    '{team_id:$team,title:"Возвраты",assignee_id:$bob}')")
TASK_ID=$(echo "$TASK" | jq -er .id)
VERSION=$(echo "$TASK" | jq -er .version)

curl -sS "$API/tasks?team_id=$TEAM_ID&status=todo&assignee_id=$BOB_ID&limit=20&offset=0" \
  -H "Authorization: Bearer $TOKEN"

curl -sS -X PUT "$API/tasks/$TASK_ID" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --argjson version "$VERSION" '{version:$version,status:"in_progress"}')"

curl -sS "$API/tasks/$TASK_ID/comments" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"Взял в работу"}'
curl -sS "$API/tasks/$TASK_ID/comments" -H "Authorization: Bearer $TOKEN"
curl -sS "$API/tasks/$TASK_ID/history" -H "Authorization: Bearer $TOKEN"
curl -sS "$API/teams/$TEAM_ID/stats" -H "Authorization: Bearer $TOKEN"
```

Ошибки имеют общий формат, например:

```json
{"error":{"code":"conflict","message":"task was changed by someone else, current version is 3"}}
```

## Решения по реализации

Хендлеры разбирают HTTP-запросы, сервисы проверяют права и управляют транзакциями, репозитории выполняют SQL. Транзакция передается через контекст: несколько операций репозиториев используют одно соединение внутри `TxManager.Do`.

| Роль | Редактирование задач | Приглашения и роли | Отчет |
| --- | --- | --- | --- |
| owner | любые задачи команды | приглашает member/admin, меняет их роли | да |
| admin | любые задачи команды | приглашает member | да |
| member | автор меняет все поля задачи, исполнитель только статус | нет | нет |

Чужая команда и ее задачи возвращают 404. Проверка участия выполняется до чтения кеша. Исполнитель должен состоять в команде задачи. Читать историю и комментировать могут все участники.

`PUT /tasks/{id}` обновляет только переданные поля. `assignee_id: null` снимает исполнителя. Клиент обязан передать `version`. Задача читается с `FOR UPDATE` и изменения коммитятся вместе с историей. На устаревшую версию сервис отвечает 409. Актуальную можно взять через `GET /tasks/{id}`. Если в запросе ничего не поменялось версия не растет. Права при этом все равно проверяются.

В истории есть запись о создании задачи. Переход в `done` ставит `closed_at`, а переоткрытие его сбрасывает. Оба случая тоже попадают в историю.

Ключ страницы в Redis включает команду, статус, исполнителя, limit, offset и поколение кеша. При изменении задачи поколение заменяется случайным токеном. Запоздавшее чтение пишет данные под прежним токеном, а старые страницы удаляются по TTL. Для каждой команды хранится один ключ поколения без срока жизни. Если он потеряется, создастся новый токен.

Отчет в `internal/repository/stats.go` использует CTE с фильтром по команде. Топ исполнителей считается по задачам закрытым за последние 30 дней включительно. При равенстве выше тот у кого меньше ID. Среднее время закрытия считается в секундах по всем закрытым задачам. Если их нет, вернется null.

Приглашение сразу добавляет зарегистрированного пользователя по email без подтверждения. Смену ролей вынес в `PUT /teams/{id}/members/{userID}`: назначать администраторов может только owner, передача роли owner не реализована.

## Проверки

```bash
make test
make lint
make test-integration
```

Последняя команда поднимает MySQL и Redis из Compose и запускает интеграционные тесты. Если Go локально не установлен:

```bash
docker compose run --rm tests
```

Для уже запущенных зависимостей:

```bash
TEST_MYSQL_DSN='root:root_password@tcp(localhost:3306)/tasks_test' \
TEST_REDIS_ADDR='localhost:6379' \
go test -tags=integration -count=1 ./...
```

ВНИМАНИЕ!!Интеграционные тесты пересоздают таблицы.!! Нужна отдельная база с именем, оканчивающимся на `_test`, и пользователь с правом ее создать. Для Redis используются отдельные случайные ключи, общая база не очищается.

Интеграционные тесты проверяют метрики отчета, изоляцию команд и границы окна в 30 дней. Еще конкурентные обновления и откат задачи если запись истории упала. И отдельно TTL, фильтры и инвалидацию кеша. Unit-тесты проверяют права и валидацию конфига.

## Ограничения

При недоступности Redis кеш и rate limit отключаются с записью в лог. Если после коммита инвалидация не прошла, в кеше до конца TTL будет лежать старый список.

Rate limit использует фиксированное окно и допускает всплеск на границе окон. Пагинация через limit/offset. Курсорную пагинацию и circuit breaker не добавлял.
