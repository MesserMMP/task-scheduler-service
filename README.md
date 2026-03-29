# Task Scheduler Service

Сервис отложенного выполнения HTTP-запросов.

Клиент создаёт задачу через API, сервис сохраняет её в PostgreSQL и выполняет в нужный момент. После выполнения сохраняется результат: HTTP-код, тело ответа, длительность. При ошибке работает retry с exponential backoff.

## Стек

- Go
- `net/http`
- PostgreSQL
- `pgx/v5`
- Docker Compose

## Запуск через Docker Compose

```bash
docker compose up --build
```

Сервис будет доступен на `http://localhost:8080`.

## Локальный запуск

1. Поднять Postgres любым удобным способом.
2. Указать env.
3. Запустить:

```bash
go mod tidy
go run .
```

## Переменные окружения

- `HTTP_PORT` (по умолчанию `8080`)
- `DATABASE_URL` (обязательно)
- `SCHEDULER_POLL_INTERVAL` (по умолчанию `1s`)
- `SCHEDULER_BATCH_SIZE` (по умолчанию `10`)
- `RETRY_BASE_DELAY` (по умолчанию `5s`)
- `REQUEST_TIMEOUT` (по умолчанию `15s`)
- `SHUTDOWN_TIMEOUT` (по умолчанию `10s`)

Пример `DATABASE_URL`:

```text
postgres://tasks:tasks@localhost:5432/tasks?sslmode=disable
```

## API

### 1) Создать задачу

`POST /tasks`

Пример запроса:

```json
{
	"url": "https://httpbin.org/post",
	"method": "POST",
	"headers": {
		"Content-Type": "application/json",
		"X-Source": "task-scheduler"
	},
	"body": "{\"hello\":\"world\"}",
	"scheduled_at": "2026-03-29T18:00:00Z",
	"max_retries": 3
}
```

### 2) Получить задачу по ID

`GET /tasks/{id}`

### 3) Список задач

`GET /tasks`

Фильтр по статусу:

`GET /tasks?status=pending`

Возможные статусы:

- `pending`
- `running`
- `completed`
- `failed`
- `cancelled`

### 4) Отменить задачу

`POST /tasks/{id}/cancel`

Отмена возможна только для задач в статусе `pending`.

## Поведение retry

- Ошибка считается при не-2xx ответе или сетевой ошибке.
- При ошибке увеличивается `attempt`.
- Если `attempt <= max_retries`, задача возвращается в `pending` и получает `next_retry_at`.
- Задержка между попытками: `RETRY_BASE_DELAY * 2^(attempt-1)`.
- Когда лимит исчерпан, задача получает статус `failed`.

## Защита от двойного выполнения

Планировщик забирает due-задачи атомарно через `FOR UPDATE SKIP LOCKED` и сразу переводит в `running` в одном SQL-выражении. Это не даёт двум конкурентным воркерам взять одну и ту же задачу.

## Graceful shutdown

При `SIGINT/SIGTERM`:

1. Останавливается приём новых HTTP-запросов.
2. Дожидаемся завершения запущенных worker goroutine.
3. Закрывается приложение.

## Тесты

Юнит-тесты бизнес-логики (с моками репозитория и HTTP-клиента):

```bash
go test ./...
```