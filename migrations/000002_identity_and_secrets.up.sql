BEGIN;

CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL,
    email_normalized text GENERATED ALWAYS AS (lower(trim(email))) STORED UNIQUE,
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 100),
    password_hash text NOT NULL,
    role text NOT NULL DEFAULT 'administrator' CHECK (role = 'administrator'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE clusters (
    id uuid PRIMARY KEY,
    name text NOT NULL UNIQUE,
    api_url text NOT NULL,
    tls_ca_pem text,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE secrets (
    id uuid PRIMARY KEY,
    cluster_id uuid REFERENCES clusters(id) ON DELETE CASCADE,
    name text NOT NULL,
    ciphertext text NOT NULL,
    key_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    rotated_at timestamptz,
    UNIQUE(cluster_id, name)
);

CREATE TABLE audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type text NOT NULL,
    user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    source_ip text NOT NULL,
    detail text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_created_at_idx ON audit_events(created_at DESC);

COMMIT;
