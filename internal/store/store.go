package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cu7ious/the-judge/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrInvalidStatus = errors.New("invalid status transition")
)

// Store provides Postgres persistence for the evaluation runner.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) Close() {
	s.pool.Close()
}

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// --- Suites ---

func (s *Store) CreateSuite(ctx context.Context, name, description string) (*domain.Suite, error) {
	var suite domain.Suite
	err := s.pool.QueryRow(ctx, `
		INSERT INTO suites (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at
	`, name, description).Scan(&suite.ID, &suite.Name, &suite.Description, &suite.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create suite: %w", err)
	}
	return &suite, nil
}

func (s *Store) ListSuites(ctx context.Context, limit int) ([]domain.Suite, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, created_at
		FROM suites
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list suites: %w", err)
	}
	defer rows.Close()

	var out []domain.Suite
	for rows.Next() {
		var suite domain.Suite
		if err := rows.Scan(&suite.ID, &suite.Name, &suite.Description, &suite.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, suite)
	}
	return out, rows.Err()
}

func (s *Store) GetSuite(ctx context.Context, id uuid.UUID) (*domain.Suite, error) {
	var suite domain.Suite
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, created_at
		FROM suites WHERE id = $1
	`, id).Scan(&suite.ID, &suite.Name, &suite.Description, &suite.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get suite: %w", err)
	}

	cases, err := s.ListTestCases(ctx, id)
	if err != nil {
		return nil, err
	}
	suite.Cases = cases
	return &suite, nil
}

// --- Test cases ---

func (s *Store) CreateTestCase(ctx context.Context, tc domain.TestCase) (*domain.TestCase, error) {
	if tc.Expected == nil {
		tc.Expected = json.RawMessage(`{}`)
	}
	if tc.Validators == nil {
		tc.Validators = json.RawMessage(`[]`)
	}
	if tc.TimeoutMs <= 0 {
		tc.TimeoutMs = 30000
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO test_cases (suite_id, name, prompt, expected, validators, timeout_ms, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, suite_id, name, prompt, expected, validators, timeout_ms, position, created_at
	`, tc.SuiteID, tc.Name, tc.Prompt, tc.Expected, tc.Validators, tc.TimeoutMs, tc.Position).Scan(
		&tc.ID, &tc.SuiteID, &tc.Name, &tc.Prompt, &tc.Expected, &tc.Validators,
		&tc.TimeoutMs, &tc.Position, &tc.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create test case: %w", err)
	}
	return &tc, nil
}

func (s *Store) ListTestCases(ctx context.Context, suiteID uuid.UUID) ([]domain.TestCase, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, suite_id, name, prompt, expected, validators, timeout_ms, position, created_at
		FROM test_cases
		WHERE suite_id = $1
		ORDER BY position ASC, created_at ASC
	`, suiteID)
	if err != nil {
		return nil, fmt.Errorf("list test cases: %w", err)
	}
	defer rows.Close()

	var out []domain.TestCase
	for rows.Next() {
		var tc domain.TestCase
		if err := rows.Scan(
			&tc.ID, &tc.SuiteID, &tc.Name, &tc.Prompt, &tc.Expected, &tc.Validators,
			&tc.TimeoutMs, &tc.Position, &tc.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (s *Store) GetTestCase(ctx context.Context, id uuid.UUID) (*domain.TestCase, error) {
	var tc domain.TestCase
	err := s.pool.QueryRow(ctx, `
		SELECT id, suite_id, name, prompt, expected, validators, timeout_ms, position, created_at
		FROM test_cases WHERE id = $1
	`, id).Scan(
		&tc.ID, &tc.SuiteID, &tc.Name, &tc.Prompt, &tc.Expected, &tc.Validators,
		&tc.TimeoutMs, &tc.Position, &tc.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get test case: %w", err)
	}
	return &tc, nil
}

// --- Runs ---

func (s *Store) CreateRun(ctx context.Context, suiteID uuid.UUID, models []domain.ModelRef, caseRuns []domain.CaseRun) (*domain.EvaluationRun, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	modelsJSON, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}

	var run domain.EvaluationRun
	err = tx.QueryRow(ctx, `
		INSERT INTO evaluation_runs (suite_id, models, status)
		VALUES ($1, $2, $3)
		RETURNING id, suite_id, models, status, created_at, started_at, finished_at, error
	`, suiteID, modelsJSON, domain.RunPending).Scan(
		&run.ID, &run.SuiteID, &modelsJSON, &run.Status, &run.CreatedAt,
		&run.StartedAt, &run.FinishedAt, &run.Error,
	)
	if err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}
	if err := json.Unmarshal(modelsJSON, &run.Models); err != nil {
		return nil, err
	}

	for i := range caseRuns {
		cr := &caseRuns[i]
		cr.RunID = run.ID
		err = tx.QueryRow(ctx, `
			INSERT INTO case_runs (run_id, test_case_id, provider, model, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, run_id, test_case_id, provider, model, status, attempt
		`, run.ID, cr.TestCaseID, cr.Provider, cr.Model, domain.CasePending).Scan(
			&cr.ID, &cr.RunID, &cr.TestCaseID, &cr.Provider, &cr.Model, &cr.Status, &cr.Attempt,
		)
		if err != nil {
			return nil, fmt.Errorf("insert case run: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) GetRun(ctx context.Context, id uuid.UUID) (*domain.EvaluationRun, error) {
	var run domain.EvaluationRun
	var modelsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, suite_id, models, status, created_at, started_at, finished_at, error
		FROM evaluation_runs WHERE id = $1
	`, id).Scan(
		&run.ID, &run.SuiteID, &modelsJSON, &run.Status, &run.CreatedAt,
		&run.StartedAt, &run.FinishedAt, &run.Error,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	if err := json.Unmarshal(modelsJSON, &run.Models); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) GetRunSummary(ctx context.Context, id uuid.UUID) (*domain.RunSummary, error) {
	run, err := s.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}

	summary := &domain.RunSummary{EvaluationRun: *run}
	rows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM case_runs WHERE run_id = $1 GROUP BY status
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status domain.CaseRunStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		summary.Total += count
		switch status {
		case domain.CasePending:
			summary.Pending = count
		case domain.CaseRunning:
			summary.Running = count
		case domain.CaseSucceeded:
			summary.Succeeded = count
		case domain.CaseFailed:
			summary.Failed = count
		case domain.CaseError:
			summary.Error = count
		case domain.CaseCancelled:
			summary.Cancelled = count
		}
	}
	return summary, rows.Err()
}

func (s *Store) ListCaseRuns(ctx context.Context, runID uuid.UUID) ([]domain.CaseRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, test_case_id, provider, model, status,
		       response_text, response_raw, usage, latency_ms, attempt, error,
		       started_at, finished_at
		FROM case_runs WHERE run_id = $1
		ORDER BY provider, model, test_case_id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CaseRun
	for rows.Next() {
		var cr domain.CaseRun
		if err := rows.Scan(
			&cr.ID, &cr.RunID, &cr.TestCaseID, &cr.Provider, &cr.Model, &cr.Status,
			&cr.ResponseText, &cr.ResponseRaw, &cr.Usage, &cr.LatencyMs, &cr.Attempt, &cr.Error,
			&cr.StartedAt, &cr.FinishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

func (s *Store) GetCaseRun(ctx context.Context, id uuid.UUID) (*domain.CaseRun, error) {
	var cr domain.CaseRun
	err := s.pool.QueryRow(ctx, `
		SELECT id, run_id, test_case_id, provider, model, status,
		       response_text, response_raw, usage, latency_ms, attempt, error,
		       started_at, finished_at
		FROM case_runs WHERE id = $1
	`, id).Scan(
		&cr.ID, &cr.RunID, &cr.TestCaseID, &cr.Provider, &cr.Model, &cr.Status,
		&cr.ResponseText, &cr.ResponseRaw, &cr.Usage, &cr.LatencyMs, &cr.Attempt, &cr.Error,
		&cr.StartedAt, &cr.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get case run: %w", err)
	}
	return &cr, nil
}

// ClaimCaseRun atomically moves pending -> running. Returns false if already claimed/terminal.
func (s *Store) ClaimCaseRun(ctx context.Context, id uuid.UUID, attempt int) (bool, error) {
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx, `
		UPDATE case_runs
		SET status = $2, started_at = COALESCE(started_at, $3), attempt = $4
		WHERE id = $1 AND status = $5
	`, id, domain.CaseRunning, now, attempt, domain.CasePending)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReclaimCaseRun allows retry after a transient failure that left status=running or pending.
func (s *Store) ReclaimCaseRun(ctx context.Context, id uuid.UUID, attempt int) (bool, error) {
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx, `
		UPDATE case_runs
		SET status = $2, started_at = COALESCE(started_at, $3), attempt = $4, error = NULL
		WHERE id = $1 AND status IN ($5, $6)
	`, id, domain.CaseRunning, now, attempt, domain.CasePending, domain.CaseRunning)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) CompleteCaseRun(ctx context.Context, id uuid.UUID, status domain.CaseRunStatus, responseText string, responseRaw, usage json.RawMessage, latencyMs int, errMsg *string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE case_runs
		SET status = $2, response_text = $3, response_raw = $4, usage = $5,
		    latency_ms = $6, error = $7, finished_at = $8
		WHERE id = $1
	`, id, status, nullIfEmpty(responseText), nullableJSON(responseRaw), nullableJSON(usage), latencyMs, errMsg, now)
	return err
}

func (s *Store) InsertValidationResults(ctx context.Context, results []domain.ValidationResult) error {
	if len(results) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, r := range results {
		if r.Details == nil {
			r.Details = json.RawMessage(`{}`)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO validation_results (case_run_id, validator_type, passed, message, details)
			VALUES ($1, $2, $3, $4, $5)
		`, r.CaseRunID, r.ValidatorType, r.Passed, r.Message, r.Details)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListValidationResults(ctx context.Context, caseRunID uuid.UUID) ([]domain.ValidationResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, case_run_id, validator_type, passed, message, details
		FROM validation_results WHERE case_run_id = $1
		ORDER BY validator_type
	`, caseRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ValidationResult
	for rows.Next() {
		var r domain.ValidationResult
		if err := rows.Scan(&r.ID, &r.CaseRunID, &r.ValidatorType, &r.Passed, &r.Message, &r.Details); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FinalizeRun recalculates and persists the parent run status from case_runs.
func (s *Store) FinalizeRun(ctx context.Context, runID uuid.UUID) (domain.RunStatus, error) {
	cases, err := s.ListCaseRuns(ctx, runID)
	if err != nil {
		return "", err
	}
	statuses := make([]domain.CaseRunStatus, len(cases))
	for i, c := range cases {
		statuses[i] = c.Status
	}
	agg := domain.AggregateRunStatus(statuses)

	now := time.Now().UTC()
	switch agg {
	case domain.RunRunning:
		_, err = s.pool.Exec(ctx, `
			UPDATE evaluation_runs
			SET status = $2, started_at = COALESCE(started_at, $3)
			WHERE id = $1 AND status IN ($4, $5)
		`, runID, domain.RunRunning, now, domain.RunPending, domain.RunRunning)
	case domain.RunCompleted, domain.RunFailed, domain.RunCancelled:
		_, err = s.pool.Exec(ctx, `
			UPDATE evaluation_runs
			SET status = $2, started_at = COALESCE(started_at, $3), finished_at = $3
			WHERE id = $1 AND status IN ($4, $5)
		`, runID, agg, now, domain.RunPending, domain.RunRunning)
	case domain.RunPending:
		// nothing
	}
	if err != nil {
		return "", err
	}
	return agg, nil
}

func (s *Store) CancelRun(ctx context.Context, runID uuid.UUID) error {
	if _, err := s.GetRun(ctx, runID); err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE evaluation_runs
		SET status = $2, finished_at = $3
		WHERE id = $1 AND status IN ($4, $5)
	`, runID, domain.RunCancelled, now, domain.RunPending, domain.RunRunning)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}

	_, err = tx.Exec(ctx, `
		UPDATE case_runs
		SET status = $2, finished_at = $3
		WHERE run_id = $1 AND status IN ($4, $5)
	`, runID, domain.CaseCancelled, now, domain.CasePending, domain.CaseRunning)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) MarkRunFailedEnqueue(ctx context.Context, runID uuid.UUID, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE evaluation_runs
		SET status = $2, error = $3, finished_at = $4
		WHERE id = $1
	`, runID, domain.RunFailed, errMsg, now)
	return err
}

func (s *Store) GetRunResults(ctx context.Context, runID uuid.UUID) ([]domain.CaseRunWithValidations, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cr.id, cr.run_id, cr.test_case_id, cr.provider, cr.model, cr.status,
		       cr.response_text, cr.response_raw, cr.usage, cr.latency_ms, cr.attempt, cr.error,
		       cr.started_at, cr.finished_at,
		       tc.name, tc.prompt
		FROM case_runs cr
		JOIN test_cases tc ON tc.id = cr.test_case_id
		WHERE cr.run_id = $1
		ORDER BY tc.position, tc.name, cr.provider, cr.model
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CaseRunWithValidations
	for rows.Next() {
		var item domain.CaseRunWithValidations
		if err := rows.Scan(
			&item.ID, &item.RunID, &item.TestCaseID, &item.Provider, &item.Model, &item.Status,
			&item.ResponseText, &item.ResponseRaw, &item.Usage, &item.LatencyMs, &item.Attempt, &item.Error,
			&item.StartedAt, &item.FinishedAt,
			&item.TestCaseName, &item.Prompt,
		); err != nil {
			return nil, err
		}
		vals, err := s.ListValidationResults(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.Validations = vals
		out = append(out, item)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
