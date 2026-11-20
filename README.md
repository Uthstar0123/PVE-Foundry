# PVE Foundry

[![CI](https://github.com/Uthstar0123/PVE-Foundry/actions/workflows/ci.yml/badge.svg)](https://github.com/Uthstar0123/PVE-Foundry/actions/workflows/ci.yml)

Локальная low-code платформа для проектирования и управления инфраструктурой Proxmox VE. Foundry формирует очередь работ, показывает OpenTofu-план и передаёт подтверждённый Apply изолированному Runner.

> Проект находится в активной разработке. Программный контур этапа 5 реализован и проверен локально с fake OpenTofu. Реальные Runner VM, `bubblewrap`, production HTTPS и Plan/Apply этой версии на тестовом Proxmox ещё не проверены. Live Apply без отдельного подтверждения владельца не запускался.

## Статус разработки

| Этап | Содержание | Статус |
|---|---|---|
| 0 | Proxmox 9.2.10, templates, Runner VM, OpenTofu smoke-тесты | ✅ Завершён отдельным smoke-контуром |
| 1 | Go services, React UI, миграции, сборка и базовые тесты | ✅ Завершён |
| 2 | PostgreSQL, первичный администратор, авторизация и защита API | ✅ Завершён |
| 3 | Read-only интеграция и инвентарь Proxmox | 🧪 Код готов, нужен фактический снимок с PVE |
| 4 | Очередь задач, логи и аудит | ✅ Реализован и проверен локально |
| 5 | Аутентифицированный Runner и безопасный OpenTofu | 🧪 Локальные проверки пройдены, нужна проверка на Runner VM |
| 6 | Конструктор и жизненный цикл ВМ/LXC | 📋 Следующий |
| 7 | Каталог и установка приложений | 📋 Запланирован |
| 8 | Backup, restore и приёмка базового MVP | 📋 Запланирован |
| 9 | Docker и Compose | 📋 После базового MVP |
| 10 | Упаковка, UX и стабилизация | 📋 После базового MVP |

Подробный план: [`docs/implementation-plan.md`](docs/implementation-plan.md). Результаты отдельной проверки Proxmox: [`docs/stage-0-compatibility.md`](docs/stage-0-compatibility.md).

### Важные ограничения текущей версии

- Визуального конструктора ВМ/LXC пока нет. В разделе «Задачи» можно загрузить самодостаточный OpenTofu `tar.gz`, получить Plan и подтвердить Apply.
- Реальный Plan/Apply нового приложения на Proxmox ещё не выполнялся. Успешные smoke-тесты этапа 0 относятся к отдельной ручной конфигурации, а не к новому Runner service.
- Production sandbox `bubblewrap`, systemd limits и HTTPS между Runner и Management проверены только как конфигурация и Linux cross-build; их нужно проверить на Runner VM.
- Artifact и бинарный план зашифрованы в PostgreSQL ключом Management. OpenTofu state находится только на Runner в каталоге с правами `0700` и **не шифруется приложением**. State может содержать секреты: его нужно защищать и включить в отдельный backup-дизайн этапа 8.
- План действует не более 15 минут. UI запрашивает подтверждение на 10 минут, но фактический срок заканчивается раньше, если истекает 15-минутный возраст плана. После истечения отмените задачу и создайте новый Plan.
- Применение привязано к SHA-256 исходного artifact и бинарного плана. Повторный claim Apply и Apply после истечения подтверждения отклоняются.
- Истёкший running lease становится `failed/lease_expired`: результат внешней операции неизвестен, автоматический повтор запрещён.
- Отмена running-задачи не является rollback. Queued/waiting-задача отменяется сразу.
- systemd-шаблон ограничивает CPU, RAM, процессы и файловые дескрипторы. Дисковую квоту нужно задавать средствами отдельной файловой системы Runner; приложение её пока не настраивает.
- Proxmox inventory кэшируется в памяти Management. PostgreSQL reconciliation и связь внутренних ресурсов с Proxmox ID ещё не реализованы.
- Панель не предназначена для прямой публикации в Интернет.

## Что уже работает

- Management API и отдельный Runner service на Go; React UI на TypeScript.
- PostgreSQL-очередь с `FOR UPDATE SKIP LOCKED`, leases, resource locks и идемпотентностью.
- Первичный администратор без пароля по умолчанию, серверные сессии, Argon2id, CSRF и rate limiting.
- Read-only Proxmox API client: только GET, доверенный CA, кэш и диагностика ошибок.
- WebSocket-снимки задач, шаги, маскирование и ротация логов, скачивание журнала, отмена и аудит.
- Исходящий канал Runner → Management: HTTPS и отдельный Bearer token; Runner не открывает административный listener.
- Последовательность `init → validate → plan → сохранение плана → подтверждение → apply`.
- Artifact и бинарный план хранятся в PostgreSQL с AES-GCM; текстовый план и события маскируются.
- Постоянный state изолирован по SHA-256 `resourceId`; временный рабочий каталог задачи удаляется.
- Проверка `tar.gz`: до 64 entries, 1 МиБ compressed, 4 МиБ uncompressed и 1 МиБ на файл; запрещены traversal, backslashes, duplicates, symlink, hardlink и специальные файлы.
- Разрешены только `.tf`, `.tf.json`, `.tfvars` и `.terraform.lock.hcl`; provider только `bpg/proxmox` версии `0.123.0`.
- Модули, provisioner и backend кроме `local` запрещены. Local backend использует постоянный state на Runner и его lock-файл.
- Служебные Runner/Management secrets удаляются из окружения дочерних процессов OpenTofu; переменные Proxmox provider сохраняются.
- Автоматические Go/API/WebSocket/runner/security-тесты, PostgreSQL integration-тесты, frontend tests/lint/build/audit и Playwright mock-сценарии.

## Архитектура

```text
Browser
   │ HTTPS / same-origin API
   ▼
Management VM
   ├─ React SPA
   ├─ Go Management API ── read-only Proxmox API + in-memory cache
   ├─ WebSocket job subscriptions
   └─ PostgreSQL ── sessions, queue, locks, logs, audit,
                    encrypted artifact + encrypted binary plan
           ▲
           │ исходящий HTTPS + Runner Bearer token
           │ (Runner проверяет TLS Management)
Runner VM  │
   ├─ polling / claim / lease renewal / cancellation
   ├─ bubblewrap + systemd resource limits
   ├─ OpenTofu + pinned bpg/proxmox provider
   ├─ временный workdir каждой задачи
   └─ /var/lib/pve-foundry/state (0700)
           │ ограниченный Proxmox API Token
           ▼
Proxmox VE
```

Management не выполняет OpenTofu. Runner не использует Docker Socket Management VM. Apply выполняется только после просмотра плана и явного подтверждения.

## Структура

```text
cmd/server/             Management API
cmd/runner/             Runner service без listening port
internal/api/           Пользовательские и Runner HTTP endpoints
internal/auth/          Argon2id и секреты
internal/runner/        Polling, sandbox, archive/provider checks
internal/seal/          AES-GCM для artifact и плана
internal/store/         PostgreSQL и изолированный test double
migrations/             SQL migrations и embedded FS
web/                    React + TypeScript + Vite
web/tests/              Playwright browser verification
deploy/runner/          systemd unit и пример production env
deploy/stage0/          Отдельный проверенный smoke-контур
docs/                   ТЗ, план и результаты совместимости
```

## Требования

- Go 1.23+;
- Node.js 20+ и npm;
- PostgreSQL 16+;
- OpenTofu для фактического Plan/Apply;
- Linux Runner с `bubblewrap` и разрешёнными user namespaces для production sandbox;
- Microsoft Edge или Chromium для Playwright;
- Proxmox VE не требуется для unit/API/browser mock-проверок.

## Быстрый старт разработки

### Windows: автоматический запуск Management и UI

Из корня проекта:

```bat
start-dev.bat
```

Батник проверяет переносимые Go/PostgreSQL и frontend-зависимости, создаёт базу `orchestrator` при отсутствии, запускает PostgreSQL `:55432`, Management `:8080` и Vite `:7100`. Он отдельно спрашивает API URL, пользователя, имя токена и секретный код, затем сам собирает `user@realm!token-id=secret`. Пустой секрет запускает панель без Proxmox.

Дополнительные режимы:

```bat
start-dev.bat -NoProxmox
start-dev.bat -SelfTest -NoProxmox -NoBrowser
```

`SelfTest` проверяет запуск и останавливает только процессы, созданные скриптом. В обычном режиме нажмите Enter в окне батника для остановки. Логи находятся в `tmp\logs`. Батник предназначен только для разработки и использует известные development-only ключи. Runner он не запускает.

### Настройка Management вручную

Создайте базу и пользователя, например:

```sql
CREATE USER orchestrator WITH PASSWORD 'replace-with-a-long-password';
CREATE DATABASE orchestrator OWNER orchestrator;
```

Скопируйте `.env.example` в `.env`. Файл не загружается автоматически: экспортируйте нужные переменные в процесс. Два разных 32-байтовых ключа создаются командой:

```bash
openssl rand -base64 32
```

- `MANAGEMENT_AUTH_KEY` — подпись CSRF-cookie;
- `MANAGEMENT_ENCRYPTION_KEY` — шифрование artifact, планов и секретов;
- `MANAGEMENT_RUNNER_TOKEN` — случайная строка не короче 32 символов для Runner;
- `MANAGEMENT_DATABASE_URL` — PostgreSQL URL.

Потеря encryption key сделает сохранённые секреты и планы нечитаемыми. В development при отсутствии ключей создаются временные значения.

Для необязательного read-only инвентаря:

```text
MANAGEMENT_PROXMOX_API_URL=https://pve.example.local:8006
MANAGEMENT_PROXMOX_API_TOKEN=foundry-ro@pve!Audit=replace-with-secret
MANAGEMENT_PROXMOX_CA_FILE=/secure/path/proxmox-ca.pem
```

`MANAGEMENT_PROXMOX_INSECURE_SKIP_VERIFY=true` допустим только в development/test и запрещён в production.

Запуск:

```bash
export MANAGEMENT_DATABASE_URL='postgres://orchestrator:replace-with-a-long-password@127.0.0.1:5432/orchestrator?sslmode=disable'
export MANAGEMENT_AUTH_KEY='base64-encoded-32-byte-key'
export MANAGEMENT_ENCRYPTION_KEY='another-base64-encoded-32-byte-key'
export MANAGEMENT_RUNNER_TOKEN='replace-with-a-long-random-runner-token'
go run ./cmd/server
```

### Запуск Runner для разработки

В другом терминале, при установленном `tofu`:

```bash
export RUNNER_ENVIRONMENT=development
export RUNNER_MANAGEMENT_URL='http://127.0.0.1:8080'
export RUNNER_RUNNER_TOKEN='replace-with-a-long-random-runner-token'
export RUNNER_OPENTOFU_BINARY='tofu'
export RUNNER_WORK_ROOT='./tmp/runner-jobs'
export RUNNER_STATE_ROOT='./tmp/runner-state'
go run ./cmd/runner
```

Runner token должен совпадать с `MANAGEMENT_RUNNER_TOKEN`. Для `bubblewrap` development не используется; production включается отдельно через `RUNNER_ENVIRONMENT=production`.

UI в третьем терминале:

```bash
npm --prefix web ci
npm --prefix web run dev
```

- UI: `http://localhost:7100/`;
- Management health: `http://localhost:8080/api/v1/healthz`;
- Management readiness: `http://localhost:8080/api/v1/readyz`.

Отдельного Runner health URL нет: `:8081` больше не слушается. Vite проксирует `/api` и `/ws` на Management.

## OpenTofu artifact

Загрузите `tar.gz` с самодостаточной конфигурацией. Не включайте `.terraform`, state, бинарный план, symlink или модули. Секрет Proxmox рекомендуется передавать через окружение Runner, а не записывать в `.tf`/`.tfvars`.

Pinned provider поддерживает:

```text
PROXMOX_VE_ENDPOINT=https://pve.example.local:8006/
PROXMOX_VE_API_TOKEN=foundry-rw@pve!OpenTofu=replace-with-secret
PROXMOX_VE_INSECURE=false
```

Описание переменных: [официальная документация bpg/proxmox](https://github.com/bpg/terraform-provider-proxmox/blob/main/docs/index.md). Используйте доверенный системный CA Runner; не включайте `insecure` в production.

## Production Runner: шаблон установки

Файлы [`deploy/runner/pve-foundry-runner.service`](deploy/runner/pve-foundry-runner.service) и [`deploy/runner/runner.env.example`](deploy/runner/runner.env.example) являются отправной точкой, а не подтверждённым unattended installer.

На выделенной Linux Runner VM:

```bash
sudo apt update
sudo apt install bubblewrap
sudo useradd --system --home /var/lib/pve-foundry --shell /usr/sbin/nologin pve-foundry
sudo install -d -o pve-foundry -g pve-foundry -m 0700 /var/lib/pve-foundry/jobs
sudo install -d -o pve-foundry -g pve-foundry -m 0700 /var/lib/pve-foundry/state
sudo install -d -o root -g pve-foundry -m 0750 /etc/pve-foundry
```

Соберите Runner и установите OpenTofu так, чтобы бинарный файл был доступен как `/usr/bin/tofu` либо измените `RUNNER_OPENTOFU_BINARY`:

```bash
GOOS=linux GOARCH=amd64 go build -o pve-foundry-runner ./cmd/runner
sudo install -o root -g root -m 0755 pve-foundry-runner /usr/local/bin/pve-foundry-runner
sudo install -o root -g root -m 0644 deploy/runner/pve-foundry-runner.service /etc/systemd/system/pve-foundry-runner.service
sudo install -o root -g pve-foundry -m 0640 deploy/runner/runner.env.example /etc/pve-foundry/runner.env
sudoedit /etc/pve-foundry/runner.env
```

Замените все example-значения и установите PEM приватного CA Management по пути из `RUNNER_MANAGEMENT_CA_FILE`. Management должен быть опубликован через HTTPS reverse proxy с соответствующим сертификатом. Затем:

```bash
sudo systemd-analyze verify /etc/systemd/system/pve-foundry-runner.service
sudo systemctl daemon-reload
sudo systemctl enable --now pve-foundry-runner
sudo systemctl status pve-foundry-runner
journalctl -u pve-foundry-runner -f
```

Перед реальным Apply отдельно проверьте `bwrap`, user namespaces, доступ к Management/Proxmox, права токена, resource limits и состояние state-каталога. Не добавляйте `RestrictNamespaces` или syscall allowlist вслепую: `bubblewrap` использует user/mount namespaces.

## Проверка и сборка

```bash
go test ./...
go vet ./...
go build ./cmd/server
go build ./cmd/runner
GOOS=linux GOARCH=amd64 go build ./cmd/runner
npm --prefix web ci
npm --prefix web test
npm --prefix web run lint
npm --prefix web run build
npm --prefix web audit
```

GitHub Actions запускает проверку форматирования Go, `go test ./...`, `go vet ./...`, сборку обоих сервисов на Ubuntu и frontend tests/lint/build/audit для каждого push и pull request. Отдельный Windows/Linux cross-build выполняется локально. Реальные Proxmox tokens для CI не нужны. Локальные Playwright и PostgreSQL integration в CI пока не запускаются.

PostgreSQL integration применяет миграции и проверяет queue, restart, leases, locks, cancel, ротацию и Plan/Apply lifecycle. Не указывайте production-базу:

```bash
TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55432/orchestrator?sslmode=disable' go test ./internal/store -run TestPostgres -v
```

Для браузерных mock-проверок с запущенным Vite:

```bash
node web/tests/inventory-preview.mjs
node web/tests/jobs-preview.mjs
node web/tests/deployments-preview.mjs
```

Сценарии не обращаются к реальному Proxmox или Runner. Они проверяют desktop/mobile layout, инвентарь, задачи, журнал, загрузку artifact, просмотр плана и привязку подтверждения к artifact SHA-256.

## API текущего этапа

Публичные:

- `GET /api/v1/healthz`, `GET /api/v1/readyz`;
- `GET /api/v1/auth/csrf`, `GET /api/v1/setup/status`;
- `POST /api/v1/setup`, `POST /api/v1/auth/login`.

Защищённые пользовательской сессией:

- `GET /api/v1/auth/session`, `POST /api/v1/auth/logout`;
- `GET /api/v1/overview`, `GET /api/v1/proxmox/inventory`;
- `GET /api/v1/jobs`, `GET /api/v1/jobs/{id}`;
- `POST /api/v1/jobs/{id}/cancel`, `POST /api/v1/jobs/{id}/approve`;
- `GET /api/v1/jobs/{id}/logs`, `GET /api/v1/audit`, `GET /ws/jobs/{id}`;
- `POST /api/v1/deployments/opentofu`.

Runner Bearer API:

- `POST /api/v1/runner/claim`;
- `POST /api/v1/runner/jobs/{id}/renew`;
- `POST /api/v1/runner/jobs/{id}/plan`;
- `POST /api/v1/runner/jobs/{id}/step`;
- `POST /api/v1/runner/jobs/{id}/event`;
- `POST /api/v1/runner/jobs/{id}/complete`.

Для пользовательских изменяющих запросов необходимы JSON, CSRF token и same-origin cookie. Runner routes используют Bearer auth вместо cookie-CSRF и сохраняют security headers.

## Production-замечания

В `MANAGEMENT_ENVIRONMENT=production` обязательны PostgreSQL URL, оба ключа и Runner token не короче 32 символов. Session/CSRF cookie становятся `Secure`. Публикация выполняется только через HTTPS. В `RUNNER_ENVIRONMENT=production` `RUNNER_MANAGEMENT_URL` обязан использовать HTTPS; приватный CA задаётся через `RUNNER_MANAGEMENT_CA_FILE`.

## Лицензия

Проект распространяется по **PolyForm Noncommercial License 1.0.0**. Полный текст находится в [`LICENSE`](LICENSE).

- разрешены личное и некоммерческое использование, изучение, изменение и распространение с сохранением лицензии;
- **коммерческое использование запрещено** без отдельного письменного разрешения правообладателя;
- это **source-available**, а не OSI open-source лицензия.

Для коммерческого лицензирования: [Uthstar0123](https://github.com/Uthstar0123). Сторонние библиотеки сохраняют собственные лицензии.

## Дальше

Следующий этап — конструктор и генерация HCL для жизненного цикла ВМ/LXC. Перед принятием этапа 5 отдельно остаются live-проверка Runner VM, production sandbox/HTTPS и фактический Plan/Apply тестовой конфигурации, а также read-only снимок реального Proxmox inventory.
