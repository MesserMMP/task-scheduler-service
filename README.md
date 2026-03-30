# Task Scheduler Service

Сервис отложенного выполнения HTTP-запросов.

Клиент создаёт задачу через API. Сервис сохраняет задачу в PostgreSQL и выполняет её в нужное время. После выполнения сохраняет результат: код ответа, тело и длительность. При ошибке запускает retry с exponential backoff.

## Что реализовано

Сделано по заданию:

- API: создание, получение, список и отмена задач
- статусы: `pending -> running -> completed/failed`, отдельно `cancelled`
- сохранение результата: код, тело, длительность, время выполнения
- retry при ошибках (не-2xx или сетевая ошибка)
- защита от двойного выполнения при конкурентном доступе
- запуск через Docker Compose
- graceful shutdown
- конфигурация через env
- README с примерами запросов
- юнит-тесты бизнес-логики с моками

## Стек

- Go
- `net/http`
- PostgreSQL
- `pgx/v5`
- Docker Compose


## Архитектура (кратко)

- [main.go](main.go) - запуск приложения
- [config.go](config.go) - env-конфиг
- [scheduler/handler.go](scheduler/handler.go) - HTTP API
- [scheduler/service.go](scheduler/service.go) - бизнес-логика
- [scheduler/repository.go](scheduler/repository.go) - интерфейс хранилища
- [scheduler/postgres_repository.go](scheduler/postgres_repository.go) - PostgreSQL и SQL
- [scheduler/model.go](scheduler/model.go) - модели и статусы
- [scheduler/service_test.go](scheduler/service_test.go) - юнит-тесты

## Запуск через Docker Compose

```bash
docker compose up --build
```

Сервис будет доступен на `http://localhost:8080`.

## Локальный запуск

1. Поднять Postgres любым удобным способом.
2. Указать env.
3. Запустить сервис:

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

Пример команды:

```bash
curl -s -X POST http://localhost:8080/tasks \
	-H "Content-Type: application/json" \
	-d '{
		"url":"https://httpbin.org/post",
		"method":"POST",
		"headers":{"Content-Type":"application/json","X-Source":"task-scheduler"},
		"body":"{\"hello\":\"world\"}",
		"scheduled_at":"2026-03-29T18:00:00Z",
		"max_retries":3
	}'
```

Ответ сервиса:

```json
{
	"id": "bea38164-7b96-43d2-b68d-79b60379d4fb",
	"url": "https://httpbin.org/post",
	"method": "POST",
	"status": "pending",
	"max_retries": 3,
	"attempt": 0,
	"scheduled_at": "2025-01-01T00:00:00Z",
	"created_at": "2026-03-30T09:38:38.078906Z",
	"updated_at": "2026-03-30T09:38:38.078906Z"
}
```

### 2) Получить задачу по ID

`GET /tasks/{id}`

Пример:

```bash
curl -s http://localhost:8080/tasks/bea38164-7b96-43d2-b68d-79b60379d4fb
```

После выполнения задачи появляются поля:

- `response_status`
- `response_body`
- `duration_ms`
- `executed_at`

Ответ сервиса:

```json
{
	"id": "bea38164-7b96-43d2-b68d-79b60379d4fb",
	"status": "completed",
	"attempt": 1,
	"response_status": 200,
	"duration_ms": 1137,
	"executed_at": "2026-03-30T09:38:39.49106Z",
	"response_body": "{ ... }"
}
```

### 3) Список задач

`GET /tasks`

Пример:

```bash
curl -s http://localhost:8080/tasks
```

Ответ сервиса:

```json
{
	"count": 2,
	"tasks": [
		{
			"id": "bea38164-7b96-43d2-b68d-79b60379d4fb",
			"status": "completed",
			"response_status": 200
		}
	],
	"timestamp": "2026-03-30T09:38:58.700580376Z"
}
```

Фильтр по статусу:

`GET /tasks?status=pending`

Пример:

```bash
curl -s "http://localhost:8080/tasks?status=completed"
```

Ответ сервиса:

```json
{
	"count": 2,
	"tasks": [
		{
			"id": "bea38164-7b96-43d2-b68d-79b60379d4fb",
			"status": "completed"
		}
	]
}
```

Возможные статусы:

- `pending`
- `running`
- `completed`
- `failed`
- `cancelled`

### 4) Отменить задачу

`POST /tasks/{id}/cancel`

Пример:

```bash
curl -s -X POST http://localhost:8080/tasks/dd6c5fb0-7423-42e2-a809-2db6dd67ddf0/cancel
```

Ответ сервиса:

```json
{"status":"cancelled"}
```

Отмена возможна только для задач в статусе `pending`.

## Защита от двойного выполнения

Планировщик забирает due-задачи атомарно через `FOR UPDATE SKIP LOCKED` и сразу переводит их в `running`. Поэтому две конкурентные ноды не возьмут одну и ту же задачу.

Ключевая идея:

- воркеры читают только `pending`
- строка блокируется на время claim
- уже захваченные строки пропускаются (`SKIP LOCKED`)

## Graceful shutdown

При `SIGINT/SIGTERM`:

1. Останавливается приём новых HTTP-запросов.
2. Дожидаемся завершения запущенных worker goroutine.
3. Закрывается приложение.

## Тесты

Юнит-тесты бизнес-логики:

```bash
go test ./...
```

Покрыты сценарии:

- успешное выполнение (`completed`)
- retry на не-2xx
- переход в `failed` после исчерпания retries на сетевой ошибке

Какие тесты есть:

- `TestExecuteTaskSuccess` - проверяет, что при 2xx задача завершается в `completed`, корректно сохраняются `attempt`, `response_status`, `response_body`
- `TestExecuteTaskRetryOnNon2xx` - проверяет, что при не-2xx задача уходит в retry и выставляется backoff (`next_retry_at` в будущем)
- `TestExecuteTaskFailWhenRetriesExhausted` - проверяет, что при сетевой ошибке и исчерпанных retry задача переходит в `failed`

В тестах используются моки:

- мок репозитория (проверка, какой transition-метод был вызван)
- мок HTTP-клиента (эмуляция успешного ответа, не-2xx и сетевой ошибки)

Подробный запуск:

```bash
go test -v ./...
```