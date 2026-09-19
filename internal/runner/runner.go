package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cu7ious/the-judge/internal/domain"
	"github.com/cu7ious/the-judge/internal/obs"
	"github.com/cu7ious/the-judge/internal/provider"
	"github.com/cu7ious/the-judge/internal/store"
	"github.com/cu7ious/the-judge/internal/validate"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// Runner executes a single case_run end-to-end.
type Runner struct {
	Store     *store.Store
	Providers provider.Registry
	Log       *slog.Logger
}

// HandleExecuteCaseRun is the asynq handler.
func (r *Runner) HandleExecuteCaseRun(ctx context.Context, task *asynq.Task) error {
	var p struct {
		CaseRunID uuid.UUID `json:"case_run_id"`
	}
	if err := json.Unmarshal(task.Payload(), &p); err != nil || p.CaseRunID == uuid.Nil {
		return fmt.Errorf("%w: invalid payload", asynq.SkipRetry)
	}
	return r.Execute(ctx, p.CaseRunID)
}

// Execute runs one case_run with idempotent claim, timeout, validate, finalize.
func (r *Runner) Execute(ctx context.Context, caseRunID uuid.UUID) error {
	cr, err := r.Store.GetCaseRun(ctx, caseRunID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: case run not found", asynq.SkipRetry)
		}
		return err
	}

	attempt, _ := asynq.GetRetryCount(ctx)
	attempt++
	log := obs.WithRunAttrs(r.Log, cr.RunID.String(), cr.ID.String(), cr.Provider, attempt)

	run, err := r.Store.GetRun(ctx, cr.RunID)
	if err != nil {
		return err
	}
	if run.Status == domain.RunCancelled {
		log.Info("skipping cancelled run")
		return nil
	}

	if cr.Status.IsTerminal() {
		log.Info("idempotent skip; already terminal", "status", cr.Status)
		return nil
	}

	var claimed bool
	if cr.Status == domain.CasePending {
		claimed, err = r.Store.ClaimCaseRun(ctx, cr.ID, attempt)
	} else {
		claimed, err = r.Store.ReclaimCaseRun(ctx, cr.ID, attempt)
	}
	if err != nil {
		return err
	}
	if !claimed {
		latest, err := r.Store.GetCaseRun(ctx, caseRunID)
		if err != nil {
			return err
		}
		if latest.Status.IsTerminal() {
			return nil
		}
		log.Info("claim lost; skipping")
		return nil
	}

	if _, err := r.Store.FinalizeRun(ctx, cr.RunID); err != nil {
		log.Warn("finalize after claim failed", "err", err)
	}

	tc, err := r.Store.GetTestCase(ctx, cr.TestCaseID)
	if err != nil {
		return r.failPermanent(ctx, cr, fmt.Sprintf("load test case: %v", err), log)
	}

	p, ok := r.Providers.Get(cr.Provider)
	if !ok {
		return r.failPermanent(ctx, cr, fmt.Sprintf("unknown provider %q", cr.Provider), log)
	}

	timeout := time.Duration(tc.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run, err = r.Store.GetRun(ctx, cr.RunID)
	if err != nil {
		return err
	}
	if run.Status == domain.RunCancelled {
		_ = r.Store.CompleteCaseRun(ctx, cr.ID, domain.CaseCancelled, "", nil, nil, 0, nil)
		_, _ = r.Store.FinalizeRun(ctx, cr.RunID)
		return nil
	}

	start := time.Now()
	resp, callErr := p.Complete(callCtx, provider.CompletionRequest{
		Model:   cr.Model,
		Prompt:  tc.Prompt,
		Timeout: timeout,
	})
	latency := int(time.Since(start).Milliseconds())

	timedOut := errors.Is(callCtx.Err(), context.DeadlineExceeded) || (resp != nil && resp.TimedOut)
	if callErr != nil && provider.IsTransient(callErr) {
		log.Warn("transient provider error", "err", callErr, "timed_out", timedOut)
		maxRetry, _ := asynq.GetMaxRetry(ctx)
		retryCount, _ := asynq.GetRetryCount(ctx)
		if retryCount >= maxRetry {
			msg := fmt.Sprintf("exhausted retries: %v", callErr)
			_ = r.Store.CompleteCaseRun(ctx, cr.ID, domain.CaseError, "", nil, nil, latency, &msg)
			_, _ = r.Store.FinalizeRun(ctx, cr.RunID)
			return fmt.Errorf("%w: %s", asynq.SkipRetry, msg)
		}
		// Keep status=running so ReclaimCaseRun works on next attempt.
		return fmt.Errorf("transient: %w", callErr)
	}

	if callErr != nil {
		msg := callErr.Error()
		var raw json.RawMessage
		text := ""
		usageJSON, _ := json.Marshal(provider.Usage{})
		if resp != nil {
			text = resp.Text
			raw = resp.Raw
			usageJSON, _ = json.Marshal(resp.Usage)
			if resp.Latency > 0 {
				latency = int(resp.Latency.Milliseconds())
			}
		}
		if err := r.Store.CompleteCaseRun(ctx, cr.ID, domain.CaseError, text, raw, usageJSON, latency, &msg); err != nil {
			return err
		}
		_, _ = r.Store.FinalizeRun(ctx, cr.RunID)
		log.Info("case run permanent provider error")
		return fmt.Errorf("%w: %v", asynq.SkipRetry, callErr)
	}

	// Cancel may have landed during the provider call.
	run, err = r.Store.GetRun(ctx, cr.RunID)
	if err != nil {
		return err
	}
	if run.Status == domain.RunCancelled {
		_ = r.Store.CompleteCaseRun(ctx, cr.ID, domain.CaseCancelled, "", nil, nil, latency, nil)
		_, _ = r.Store.FinalizeRun(ctx, cr.RunID)
		return nil
	}

	usageJSON, _ := json.Marshal(resp.Usage)
	text := resp.Text

	valInput := validate.Input{
		ResponseText: text,
		TimedOut:     timedOut,
		HadError:     false,
		Expected:     tc.Expected,
	}
	results, err := validate.RunAll(ctx, tc.Validators, valInput)
	if err != nil {
		return r.failPermanent(ctx, cr, fmt.Sprintf("validators: %v", err), log)
	}

	domainResults := make([]domain.ValidationResult, 0, len(results))
	for _, res := range results {
		domainResults = append(domainResults, domain.ValidationResult{
			CaseRunID:     cr.ID,
			ValidatorType: res.Type,
			Passed:        res.Passed,
			Message:       res.Message,
			Details:       res.Details,
		})
	}
	if err := r.Store.InsertValidationResults(ctx, domainResults); err != nil {
		return err
	}

	status := domain.CaseSucceeded
	if !validate.AllPassed(results) {
		status = domain.CaseFailed
	}
	if err := r.Store.CompleteCaseRun(ctx, cr.ID, status, text, resp.Raw, usageJSON, latency, nil); err != nil {
		return err
	}
	if _, err := r.Store.FinalizeRun(ctx, cr.RunID); err != nil {
		log.Warn("finalize failed", "err", err)
	}

	log.Info("case run finished", "status", status, "latency_ms", latency)
	return nil
}

func (r *Runner) failPermanent(ctx context.Context, cr *domain.CaseRun, msg string, log *slog.Logger) error {
	if err := r.Store.CompleteCaseRun(ctx, cr.ID, domain.CaseError, "", nil, nil, 0, &msg); err != nil {
		return err
	}
	_, _ = r.Store.FinalizeRun(ctx, cr.RunID)
	log.Error("permanent failure", "err", msg)
	return fmt.Errorf("%w: %s", asynq.SkipRetry, msg)
}
