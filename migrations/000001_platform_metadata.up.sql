BEGIN;

CREATE TABLE platform_metadata (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE platform_metadata IS 'Non-secret platform metadata; credentials are stored separately in stage 2.';

COMMIT;
