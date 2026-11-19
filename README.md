# PVE Foundry

[![CI](https://github.com/Uthstar0123/PVE-Foundry/actions/workflows/ci.yml/badge.svg)](https://github.com/Uthstar0123/PVE-Foundry/actions/workflows/ci.yml)

Локальная low-code платформа для проектирования и управления инфраструктурой Proxmox VE. Пользователь собирает ВМ, LXC и сети блоками, а Foundry формирует OpenTofu-конфигурацию, показывает план изменений и передаёт подтверждённую работу изолированному Runner.

> Проект находится в активной разработке. Этапы 0–2 завершены. Для этапа 3 реализованы read-only API, кэш и интерфейс инвентаря; фактическая синхронизация с реальным Proxmox ещё должна быть проверена с API token владельца.

## Статус разработки

**Текущая точка:** реализован программный контур этапа 3. Unit/API-тесты, сборка и браузерные сценарии с mock Proxmox проходят. Это не production-релиз: критерий показа фактического состояния тестового Proxmox пока не закрыт.

| Этап | Содержание | Статус |
|---|---|---|
| 0 | Proxmox 9.2.10, templates, Runner VM, OpenTofu smoke-тесты | ✅ Завершён |
| 1 | Go services, React UI, миграции, сборка и базовые тесты | ✅ Завершён |
| 2 | PostgreSQL, первичный администратор, авторизация и защита API | ✅ Завершён |
| 3 | Read-only интеграция и инвентарь Proxmox | 🧪 Код готов, нужна проверка на реальном PVE |
| 4 | Очередь задач, логи и аудит | 📋 Запланирован |
| 5 | Аутентифицированный Runner и безопасный OpenTofu | 📋 Запланирован |
| 6 | Конструктор и жизненный цикл ВМ/LXC | 📋 Запланирован |
| 7 | Каталог и установка приложений | 📋 Запланирован |
| 8 | Backup, restore и приёмка базового MVP | 📋 Запланирован |
| 9 | Docker и Compose | 📋 После базового MVP |
| 10 | Упаковка, UX и стабилизация | 📋 После базового MVP |

Подробный план: [`docs/implementation-plan.md`](docs/implementation-plan.md). Фактические результаты проверки Proxmox: [`docs/stage-0-compatibility.md`](docs/stage-0-compatibility.md).

### Важные ограничения текущей версии

- UI не создаёт, не изменяет и не удаляет ресурсы Proxmox.
- Runner предоставляет только health endpoints и не принимает задания.
- Proxmox подключается через переменные окружения Management; защищённая страница настроек кластера ещё не реализована.
- Снимок инвентаря кэшируется в памяти на 30 секунд и обновляется в фоне раз в минуту. PostgreSQL-кэш и reconciliation внутренних ресурсов ещё не реализованы.
- TLS, формат ответов и ошибки API проверены на mock server. Фактический снимок с PVE 9.2.10 не запрашивался этим кодом, потому что секрет токена недоступен автоматическим проверкам.
- Smoke-тесты этапа 0 создавали только ресурсы тестового pool; рабочие ресурсы не изменялись.

## Что уже работает

- Проверенный тестовый контур Proxmox VE 9.2.10.
- Создание и удаление тестовых QEMU/LXC через OpenTofu 1.13.9 и `bpg/proxmox` 0.123.0 — проверено на этапе 0.
- Management API и отдельный Runner service на Go.
- PostgreSQL с версионируемыми встроенными миграциями.
- Мастер единственного первичного администратора без пароля по умолчанию.
- Вход, выход и серверные сессии со сроком действия 12 часов.
- Argon2id для паролей и AES-256-GCM для секретов.
- CSRF-защита, `HttpOnly`/`SameSite=Strict` cookie и ограничение перебора пароля.
- Авторизованный `GET /api/v1/proxmox/inventory`: версия, ноды, QEMU/LXC, storage, pools и сетевые интерфейсы.
- Proxmox API client использует только GET, поддерживает доверенный CA и не раскрывает token в ответах API.
- Кэш инвентаря в памяти, объединение одновременных запросов и фоновое обновление.
- React UI с настройкой, входом, обзором нод, раскрываемыми карточками ресурсов, storage, pools и сетью.
- Состояния загрузки, отсутствия конфигурации и ошибки с повторным запросом.
- Автоматические unit/API-тесты и Playwright-сценарии реальной авторизации и mock-инвентаря.

## Архитектура

```text
Browser
   │ HTTPS / same-origin API
   ▼
Management VM
   ├─ React SPA
   ├─ Go Management API ── read-only Proxmox API + in-memory cache
   └─ PostgreSQL
           │ аутентифицированный канал — этап 5
           ▼
Runner VM
   ├─ OpenTofu
   ├─ Ansible
   └─ отдельный state и рабочий каталог каждого deployment
           │ Proxmox API Token с ограниченными правами
           ▼
Proxmox VE
```

Management не выполняет внешние конфигурации. Runner не использует Docker Socket Management VM. Apply всегда должен выполняться после просмотра плана и явного подтверждения.

## Структура

```text
cmd/server/             Management API
cmd/runner/             Runner service
internal/api/           HTTP API, sessions, CSRF, rate limiting
internal/auth/          Argon2id и шифрование секретов
internal/config/        Конфигурация процесса
internal/proxmox/       Read-only client, TLS и in-memory cache
internal/store/         PostgreSQL и изолированный test double
migrations/             SQL migrations и embedded FS
web/                    React + TypeScript + Vite
web/tests/              Playwright browser verification
deploy/stage0/          Проверенный smoke-контур
docs/                   ТЗ, план и результаты совместимости
```

## Требования

- Go 1.23+;
- Node.js 20+ и npm;
- PostgreSQL 16+;
- Microsoft Edge или Chromium для Playwright browser check;
- Proxmox VE не требуется для unit/API-тестов и браузерной проверки с mock;
- для фактической синхронизации — Proxmox VE и read-only API token с `PVEAuditor`.

## Быстрый старт разработки

### Windows: автоматический запуск

Из корня проекта:

```bat
start-dev.bat
```

Батник проверяет переносимые Go/PostgreSQL и frontend-зависимости, создаёт базу `orchestrator` при отсутствии, запускает PostgreSQL `:55432`, Management `:8080` и Vite `:7100`, затем открывает панель. Батник отдельно спрашивает API URL, пользователя, имя токена и секретный код, после чего сам собирает `user@realm!token-id=secret`. Локально заданный `MANAGEMENT_PROXMOX_API_URL` используется как подсказка для адреса. Нейтральные значения по умолчанию — `https://pve.example.local:8006`, `foundry-ro@pve` и `Audit`; для `foundry-rw@pve` автоматически предлагается `OpenTofu`. Секрет вводится скрыто, пустой ввод запускает панель без Proxmox. Реальные параметры инфраструктуры в скрипт и Git не записываются.

Дополнительные режимы:

```bat
start-dev.bat -NoProxmox
start-dev.bat -SelfTest -NoProxmox -NoBrowser
```

`SelfTest` проверяет запуск и автоматически останавливает только процессы, созданные скриптом. В обычном режиме нажмите Enter в окне батника для остановки. Логи находятся в `tmp\logs`. Батник предназначен только для локальной разработки и использует тестовые ключи; для self-signed Proxmox включает insecure TLS только в development.

Ниже описан ручной вариант запуска.

### 1. Создайте базу и пользователя

Пример для локального PostgreSQL:

```sql
CREATE USER orchestrator WITH PASSWORD 'replace-with-a-long-password';
CREATE DATABASE orchestrator OWNER orchestrator;
```

### 2. Настройте окружение

Скопируйте `.env.example` в `.env` и задайте собственные значения. Файл `.env` игнорируется Git. Переменные окружения экспортируются в процесс; `.env` является примером конфигурации, а не автоматически загружаемым файлом.

Сгенерировать 32-байтовые ключи можно так:

```bash
openssl rand -base64 32
```

Нужны два разных ключа:

- `MANAGEMENT_AUTH_KEY` — подпись CSRF-cookie;
- `MANAGEMENT_ENCRYPTION_KEY` — шифрование секретов приложения.

Ключ шифрования нужно хранить отдельно от резервной копии базы. Его потеря сделает сохранённые секреты нечитаемыми. В development при отсутствии ключей создаются временные значения: после перезапуска старые сессии и зашифрованные секреты становятся недействительными.

Для необязательного подключения read-only инвентаря задайте обе переменные:

```text
MANAGEMENT_PROXMOX_API_URL=https://pve.example.local:8006
MANAGEMENT_PROXMOX_API_TOKEN=user@pve!token-id=secret
```

Не отправляйте реальный secret в чат и не записывайте его в Git. Для приватного или самоподписанного CA укажите PEM-файл:

```text
MANAGEMENT_PROXMOX_CA_FILE=/secure/path/proxmox-ca.pem
```

`MANAGEMENT_PROXMOX_INSECURE_SKIP_VERIFY=true` допустим только в development/test и запрещён в production. Без URL и token Management запускается обычно, а endpoint инвентаря возвращает `503 proxmox_not_configured`.

### 3. Запустите Management и Runner

```bash
export MANAGEMENT_DATABASE_URL='postgres://orchestrator:replace-with-a-long-password@127.0.0.1:5432/orchestrator?sslmode=disable'
export MANAGEMENT_AUTH_KEY='base64-encoded-32-byte-key'
export MANAGEMENT_ENCRYPTION_KEY='another-base64-encoded-32-byte-key'
go run ./cmd/server
```

В другом терминале:

```bash
go run ./cmd/runner
```

### 4. Запустите UI

```bash
npm --prefix web ci
npm --prefix web run dev
```

- UI: `http://localhost:7100/`;
- Management health: `http://localhost:8080/api/v1/healthz`;
- Management readiness: `http://localhost:8080/api/v1/readyz`;
- Runner health: `http://localhost:8081/api/v1/healthz`.

Vite проксирует `/api` на Management. При первом открытии создайте администратора; после этого мастер первичной настройки закрывается.

## Проверка и сборка

```bash
go test ./...
go vet ./...
go build ./cmd/server
go build ./cmd/runner
npm --prefix web ci
npm --prefix web test
npm --prefix web run lint
npm --prefix web run build
npm --prefix web audit
```

GitHub Actions автоматически запускает проверку форматирования Go, `go test ./...`, `go vet ./...`, сборку обоих сервисов, frontend tests/lint/build и `npm audit` для каждого push и pull request. Конфигурация находится в `.github/workflows/ci.yml`; реальные Proxmox tokens для CI не нужны. Локальные Playwright-сценарии с Edge и PostgreSQL в CI пока не запускаются.

Для сквозного сценария авторизации нужны запущенные PostgreSQL, Management на `:8080` и UI на `:7100`:

```bash
node web/tests/preview.mjs
```

Сценарий ожидает пустую базу и выполняет `setup → logout → login → logout`. Не запускайте его на установке с реальным администратором.

Для визуальной проверки инвентаря достаточно запущенного Vite UI:

```bash
node web/tests/inventory-preview.mjs
```

Этот сценарий перехватывает API в Playwright и использует **mock-данные, не реальный Proxmox**. Он проверяет desktop/mobile layout, навигацию, карточку ресурса, обновление и состояние `proxmox_not_configured`.

## API текущего этапа

Публичные:

- `GET /api/v1/healthz`;
- `GET /api/v1/readyz`;
- `GET /api/v1/auth/csrf`;
- `GET /api/v1/setup/status`;
- `POST /api/v1/setup`;
- `POST /api/v1/auth/login`.

Защищённые серверной сессией:

- `GET /api/v1/auth/session`;
- `POST /api/v1/auth/logout`;
- `GET /api/v1/overview`;
- `GET /api/v1/proxmox/inventory`.

Для всех изменяющих запросов необходимы JSON, CSRF token в `X-CSRF-Token` и same-origin cookie. Пароли, password hash, Proxmox token и encryption key не включаются в ответы API. Клиент Proxmox выполняет только GET-запросы.

## Production-замечания

В `MANAGEMENT_ENVIRONMENT=production` обязательны PostgreSQL URL и оба ключа. Session/CSRF cookie становятся `Secure`. Публикацию следует выполнять только через HTTPS и без открытия панели напрямую в Интернет. Для Proxmox API используйте доверенный CA через `MANAGEMENT_PROXMOX_CA_FILE`; отключение проверки TLS в production отклоняется при загрузке конфигурации.

## Лицензия

Проект распространяется по **PolyForm Noncommercial License 1.0.0**. Полный текст находится в [`LICENSE`](LICENSE).

Простыми словами:

- разрешены личное и некоммерческое использование, изучение, изменение и распространение кода с сохранением лицензии и уведомления об авторских правах;
- **коммерческое использование запрещено** без отдельного письменного разрешения правообладателя;
- продажа, включение в коммерческий продукт и использование для получения коммерческой выгоды требуют отдельной лицензии;
- это **source-available**, а не OSI open-source лицензия.

Для коммерческого лицензирования необходимо связаться с правообладателем: [Uthstar0123](https://github.com/Uthstar0123).

Сторонние библиотеки и ресурсы сохраняют собственные лицензии.

## Дальше

Сначала нужно подтвердить этап 3 фактическим read-only снимком тестового Proxmox и решить, нужен ли PostgreSQL-кэш для reconciliation. Затем начинается этап 4: PostgreSQL-backed очередь, задачи, шаги, leases, логи и аудит.
