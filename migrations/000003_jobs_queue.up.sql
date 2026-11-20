BEGIN;

CREATE TABLE jobs (
    id uuid PRIMARY KEY,
    operation text NOT NULL CHECK (char_length(operation) BETWEEN 1 AND 100),
    resource_id text NOT NULL CHECK (char_length(resource_id) BETWEEN 1 AND 255),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'waiting_approval', 'running', 'succeeded', 'failed', 'cancelled')),
    initiator_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    request_hash text NOT NULL CHECK (char_length(request_hash) = 64),
    worker_id text,
    lease_expires_at timestamptz,
    cancel_requested_at timestamptz,
    error_code text,
    logs_truncated boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(initiator_user_id, idempotency_key),
    CHECK (status <> 'running' OR (worker_id IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CHECK (worker_id IS NULL OR char_length(worker_id) BETWEEN 1 AND 255)
);
CREATE INDEX jobs_created_at_idx ON jobs(created_at DESC);
CREATE INDEX jobs_claim_idx ON jobs(created_at, id) WHERE status = 'queued';
CREATE INDEX jobs_lease_idx ON jobs(lease_expires_at) WHERE status = 'running';

CREATE TABLE resource_locks (
    resource_id text PRIMARY KEY CHECK (char_length(resource_id) BETWEEN 1 AND 255),
    job_id uuid NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX resource_locks_job_id_idx ON resource_locks(job_id);

CREATE TABLE job_steps (
    id uuid PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    status text NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped', 'cancelled')),
    detail text NOT NULL DEFAULT '',
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(job_id, name)
);
CREATE INDEX job_steps_job_id_idx ON job_steps(job_id, created_at, id);

CREATE TABLE job_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    level text NOT NULL CHECK (level IN ('debug', 'info', 'warn', 'error')),
    message text NOT NULL CHECK (char_length(message) <= 16384),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX job_events_job_id_idx ON job_events(job_id, id DESC);

COMMIT;
