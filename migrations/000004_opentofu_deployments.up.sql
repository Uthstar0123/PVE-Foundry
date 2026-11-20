BEGIN;

CREATE TABLE deployment_artifacts (
    job_id uuid PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    artifact bytea NOT NULL,
    artifact_sha256 text NOT NULL CHECK (char_length(artifact_sha256) = 64),
    plan_text text,
    plan_file bytea,
    plan_sha256 text CHECK (plan_sha256 IS NULL OR char_length(plan_sha256) = 64),
    plan_created_at timestamptz,
    approval_expires_at timestamptz,
    approved_by uuid REFERENCES users(id) ON DELETE RESTRICT,
    approved_at timestamptz,
    CHECK ((plan_file IS NULL) = (plan_sha256 IS NULL)),
    CHECK ((approved_by IS NULL) = (approved_at IS NULL))
);
CREATE INDEX deployment_artifacts_claim_approved_idx ON deployment_artifacts(approved_at, job_id) WHERE approved_at IS NOT NULL;

COMMIT;
