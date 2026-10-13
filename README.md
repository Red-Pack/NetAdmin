# Remote Admin — MVP vertical slice

Первый проверяемый срез: Go API, PostgreSQL, регистрация Windows-агента, heartbeat и список устройств.

## Что реализовано

- `GET /healthz` — проверка процесса и БД.
- `POST /api/v1/agents/register` — регистрация агента по bootstrap-токену, выдача ID устройства и индивидуального секрета.
- `POST /api/v1/agents/heartbeat` — проверка индивидуального секрета и обновление системной информации/времени последнего heartbeat.
- `GET /api/v1/devices` — список устройств.
- Идемпотентная стартовая миграция PostgreSQL, Docker Compose, тесты логики токенов и статусов.
- Windows-агент с сохранением выданных реквизитов в JSON-файл и периодическим heartbeat.

## Важные ограничения прототипа

**Не выставляйте этот прототип в недоверенную сеть и не используйте для администрирования реальных рабочих станций.**

- Для простоты API пока использует HTTP. Перед эксплуатацией нужен TLS; для агентов предпочтителен mTLS или короткоживущие токены с ротацией.
- Bootstrap-токен общий для первичной регистрации. Его необходимо хранить как секрет и ротировать.
- Секрет агента показывается только при регистрации, но в MVP хранится локально в JSON-файле без защищённого хранилища ОС.
- Нет пользовательской аутентификации/RBAC, аудита административных действий, отзыва секретов, защиты от повторов на уровне подписанных запросов и rate limiting.
- `/api/v1/devices` пока не защищён авторизацией.
- Heartbeat пока работает по HTTP, а не через постоянный WebSocket. Это намеренное упрощение первого среза; WebSocket и подтверждение команд следует добавлять после проверки регистрации/heartbeat.
- Агент пока консольное приложение, не Windows Service; Linux-агент, обновление агента, команды и удалённый рабочий стол отсутствуют.
- IP берётся из удалённого адреса HTTP-запроса и не считается достоверным при работе за прокси без корректной настройки доверенных proxy.
- Офлайн-статус вычисляется при чтении списка устройств: `now - last_seen > OFFLINE_AFTER`.

## Запуск сервера

Требования: Docker Compose, Go 1.24+.

```bash
cp .env.example .env
# Измените AGENT_ENROLLMENT_TOKEN и пароль БД в .env.
docker compose -f deploy/docker-compose.yml up --build -d
```

Сервер применяет идемпотентную стартовую миграцию при запуске. Compose использует переменные из `.env` автоматически только если файл находится в текущем каталоге и передан `--env-file` при запуске из другого каталога. Для однозначного запуска:

```bash
docker compose --env-file .env -f deploy/docker-compose.yml up --build -d
```

Проверка:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/devices
```

## Сборка и запуск Windows-агента

Из корня репозитория:

```bash
go mod tidy
make agent-windows
```

На Windows можно собрать напрямую:

```powershell
go build -o .\bin\remote-admin-agent.exe .\apps\agent-windows
```

Запуск (PowerShell):

```powershell
$env:SERVER_URL = "http://SERVER-HOST:8080"
$env:ENROLLMENT_TOKEN = "тот-же-токен-что-в-.env"
$env:AGENT_STATE_FILE = "$env:ProgramData\RemoteAdmin\agent-state.json"
.\bin\remote-admin-agent.exe
```

Для тестирования без установки в ProgramData можно задать путь в доступную пользователю папку. Агент создаёт каталог для файла состояния автоматически.

## API

### Register

`POST /api/v1/agents/register`

```json
{
  "enrollment_token": "bootstrap-token",
  "hostname": "WS-001",
  "os": "windows",
  "architecture": "amd64",
  "agent_version": "0.1.0"
}
```

Ответ `201 Created` содержит `device_id` и `agent_secret`. Секрет необходимо сохранить: повторно он не выдаётся.

### Heartbeat

`POST /api/v1/agents/heartbeat`

Заголовок: `Authorization: Bearer <agent_secret>`

```json
{
  "hostname": "WS-001",
  "os": "windows",
  "architecture": "amd64",
  "agent_version": "0.1.0"
}
```

### Device list

`GET /api/v1/devices`

В ответе поле `status` — `online` или `offline`, вычисленное по `last_seen_at`.

## Тесты

```bash
go mod tidy
go test ./...
```

Интеграционные тесты с PostgreSQL пока не включены; перед production необходимо добавить тесты миграций, регистрации/heartbeat на реальной тестовой БД, TLS/mTLS и проверки авторизации.
