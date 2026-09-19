-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE suites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE test_cases (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    suite_id    UUID NOT NULL REFERENCES suites(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    expected    JSONB NOT NULL DEFAULT '{}',
    validators  JSONB NOT NULL DEFAULT '[]',
    timeout_ms  INT NOT NULL DEFAULT 30000,
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_test_cases_suite_id ON test_cases(suite_id);

CREATE TABLE evaluation_runs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    suite_id    UUID NOT NULL REFERENCES suites(id) ON DELETE CASCADE,
    models      JSONB NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error       TEXT
);

CREATE INDEX idx_evaluation_runs_suite_id ON evaluation_runs(suite_id);
CREATE INDEX idx_evaluation_runs_status ON evaluation_runs(status);

CREATE TABLE case_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    test_case_id    UUID NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,
    model           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    response_text   TEXT,
    response_raw    JSONB,
    usage           JSONB,
    latency_ms      INT,
    attempt         INT NOT NULL DEFAULT 0,
    error           TEXT,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);

CREATE INDEX idx_case_runs_run_id ON case_runs(run_id);
CREATE INDEX idx_case_runs_status ON case_runs(status);
CREATE UNIQUE INDEX idx_case_runs_unique ON case_runs(run_id, test_case_id, provider, model);

CREATE TABLE validation_results (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_run_id     UUID NOT NULL REFERENCES case_runs(id) ON DELETE CASCADE,
    validator_type  TEXT NOT NULL,
    passed          BOOLEAN NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    details         JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_validation_results_case_run_id ON validation_results(case_run_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS validation_results;
DROP TABLE IF EXISTS case_runs;
DROP TABLE IF EXISTS evaluation_runs;
DROP TABLE IF EXISTS test_cases;
DROP TABLE IF EXISTS suites;
-- +goose StatementEnd
