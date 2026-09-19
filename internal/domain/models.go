package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Suite is a named collection of test cases.
type Suite struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Cases       []TestCase `json:"cases,omitempty"`
}

// TestCase is a single prompt + expected validators within a suite.
type TestCase struct {
	ID         uuid.UUID       `json:"id"`
	SuiteID    uuid.UUID       `json:"suite_id"`
	Name       string          `json:"name"`
	Prompt     string          `json:"prompt"`
	Expected   json.RawMessage `json:"expected"`
	Validators json.RawMessage `json:"validators"`
	TimeoutMs  int             `json:"timeout_ms"`
	Position   int             `json:"position"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ModelRef identifies a provider + model for a run.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// EvaluationRun is a submitted execution of a suite against one or more models.
type EvaluationRun struct {
	ID         uuid.UUID  `json:"id"`
	SuiteID    uuid.UUID  `json:"suite_id"`
	Models     []ModelRef `json:"models"`
	Status     RunStatus  `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      *string    `json:"error,omitempty"`
}

// CaseRun is one (test_case × model) execution unit.
type CaseRun struct {
	ID           uuid.UUID       `json:"id"`
	RunID        uuid.UUID       `json:"run_id"`
	TestCaseID   uuid.UUID       `json:"test_case_id"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	Status       CaseRunStatus   `json:"status"`
	ResponseText *string         `json:"response_text,omitempty"`
	ResponseRaw  json.RawMessage `json:"response_raw,omitempty"`
	Usage        json.RawMessage `json:"usage,omitempty"`
	LatencyMs    *int            `json:"latency_ms,omitempty"`
	Attempt      int             `json:"attempt"`
	Error        *string         `json:"error,omitempty"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
}

// ValidationResult is the outcome of one deterministic validator.
type ValidationResult struct {
	ID            uuid.UUID       `json:"id"`
	CaseRunID     uuid.UUID       `json:"case_run_id"`
	ValidatorType string          `json:"validator_type"`
	Passed        bool            `json:"passed"`
	Message       string          `json:"message"`
	Details       json.RawMessage `json:"details,omitempty"`
}

// RunSummary is a lightweight view of run progress.
type RunSummary struct {
	EvaluationRun
	Total        int  `json:"total"`
	Pending      int  `json:"pending"`
	Running      int  `json:"running"`
	Succeeded    int  `json:"succeeded"`
	Failed       int  `json:"failed"`
	Error        int  `json:"error_count"`
	Cancelled    int  `json:"cancelled"`
	MaxLatencyMs *int `json:"max_latency_ms,omitempty"`
}

// CaseRunWithValidations is used by the compare/results API.
type CaseRunWithValidations struct {
	CaseRun
	TestCaseName string             `json:"test_case_name"`
	Prompt       string             `json:"prompt"`
	Validations  []ValidationResult `json:"validations"`
}
