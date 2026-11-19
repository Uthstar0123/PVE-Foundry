# PVE Foundry

Локальная low-code платформа для проектирования и управления инфраструктурой Proxmox VE. Пользователь собирает ВМ, LXC и сети блоками, а Foundry формирует OpenTofu-конфигурацию, показывает план изменений и передаёт подтверждённую работу изолированному Runner.

> Проект находится в активной разработке. Этапы 0–2 реализованы. Создание ресурсов через UI, каталог приложений и запуск OpenTofu из панели будут добавлены следующими этапами.

## Статус разработки

**Текущая точка:** этап 2 завершён 19 ноября 2026 года; следующий этап — read-only инвентарь Proxmox. Проект ещё не является готовым production-релизом.

| Этап | Содержание | Статус |
|---|---|---|
| 0 | Proxmox 9.2.10, templates, Runner VM, OpenTofu smoke-тесты | ✅ Завершён |
| 1 | Go services, React UI, миграции, сборка и базовые тесты | ✅ Завершён |
| 2 | PostgreSQL, первичный администратор, авторизация и защита API | ✅ Завершён |
| 3 | Read-only интеграция и инвентарь Proxmox | ⏭️ Следующий |
| 4 | Очередь задач, логи и аудит | 📋 Запланирован |
| 5 | Аутентифицированный Runner и безопасный OpenTofu | 📋 Запланирован |
| 6 | Конструктор и жизненный цикл ВМ/LXC | 📋 Запланирован |
| 7 | Каталог и установка приложений | 📋 Запланирован |
| 8 | Backup, restore и приёмка базового MVP | 📋 Запланирован |
| 9 | Docker и Compose | 📋 После базового MVP |
| 10 | Упаковка, UX и стабилизация | 📋 После базового MVP |

Подробный план: [`docs/implementation-plan.md`](docs/implementation-plan.md). Фактические результаты проверки Proxmox: [`docs/stage-0-compatibility.md`](docs/stage-0-compatibility.md).

### Важные ограничения текущей версии

- UI пока не создаёт, не изменяет и не удаляет ресурсы Proxmox.
- Runner сейчас предоставляет только health endpoints и не принимает задания.
- Подключение кластера через защищённые настройки появится на этапе 3.
- TLS-проверка Proxmox CA, очередь задач, backup и recovery ещё не реализованы.
- Smoke-тесты создавали только ресурсы тестового pool; рабочие ресурсы не изменялись.

## Что уже работает

- Проверенный тестовый контур Proxmox VE 9.2.10.
- Создание и удаление тестовых QEMU/LXC через OpenTofu 1.13.9 и `bpg/proxmox` 0.123.0.
- Management API и отдельный Runner service на Go.
- PostgreSQL с версионируемыми встроенными миграциями.
- Мастер единственного первичного администратора без пароля по умолчанию.
- Вход, выход и серверные сессии со сроком действия 12 часов.
- Argon2id для паролей и AES-256-GCM для секретов.
- CSRF-защита, `HttpOnly`/`SameSite=Strict` cookie и ограничение перебора пароля.
- React UI с адаптивными формами первичной настройки и входа.
- Автоматические unit/API-тесты и сквозной Playwright-сценарий.

## Архитектура

```text
Browser
   │ HTTPS / same-origin API
   ▼
Management VM
   ├─ React SPA
   ├─ Go Management API
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
- Proxmox VE не требуется для локальных проверок этапов 1–2.

## Быстрый старт разработки

### 1. Создайте базу и пользователя

Пример для локального PostgreSQL:

```sql
CREATE USER orchestrator WITH PASSWORD 'replace-with-a-long-password';
CREATE DATABASE orchestrator OWNER orchestrator;
```

### 2. Настройте окружение

Скопируйте `.env.example` в `.env` и задайте собственные значения. Файл `.env` игнорируется Git.

Сгенерировать 32-байтовые ключи можно так:

```bash
openssl rand -base64 32
```

Нужны два разных ключа:

- `MANAGEMENT_AUTH_KEY` — подпись CSRF-cookie;
- `MANAGEMENT_ENCRYPTION_KEY` — шифрование секретов приложения.

Ключ шифрования нужно хранить отдельно от резервной копии базы. Его потеря сделает сохранённые секреты нечитаемыми. В development при отсутствии ключей создаются временные значения: после перезапуска старые сессии и зашифрованные секреты становятся недействительными.

Переменные окружения экспортируются в процесс; `.env` является примером конфигурации, а не автоматически загружаемым файлом.

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

Для сквозного сценария нужны запущенные PostgreSQL, Management на `:8080` и UI на `:7100`:

```bash
node web/tests/preview.mjs
```

Сценарий ожидает пустую базу и выполняет `setup → logout → login → logout`. Не запускайте его на установке с реальным администратором.

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
- `GET /api/v1/overview`.

Для всех изменяющих запросов необходимы JSON, CSRF token в `X-CSRF-Token` и same-origin cookie. Пароли, password hash, Proxmox token и encryption key не включаются в ответы API.

## Production-замечания

В `MANAGEMENT_ENVIRONMENT=production` обязательны PostgreSQL URL и оба ключа. Session/CSRF cookie становятся `Secure`. Публикацию следует выполнять только через HTTPS и без открытия панели напрямую в Интернет. TLS Proxmox API с `insecure = true` остаётся временным ограничением smoke-контура и будет заменён проверкой CA на этапе 3.

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

Следующий этап — read-only интеграция с Proxmox: TLS diagnostics, ноды, ресурсы, storage, pools, сеть и синхронизация без изменяющих операций.
