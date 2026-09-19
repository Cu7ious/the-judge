package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	TypeExecuteCaseRun = "case_run:execute"
	QueueDefault       = "default"
)

// ExecuteCaseRunPayload identifies the case_run to execute.
type ExecuteCaseRunPayload struct {
	CaseRunID uuid.UUID `json:"case_run_id"`
}

// Enqueuer wraps asynq.Client.
type Enqueuer struct {
	client     *asynq.Client
	maxRetries int
}

func NewEnqueuer(redisAddr string, maxRetries int) *Enqueuer {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return &Enqueuer{client: client, maxRetries: maxRetries}
}

func (e *Enqueuer) Close() error {
	return e.client.Close()
}

func (e *Enqueuer) Ping() error {
	// asynq client does not expose ping; rely on Redis via inspector or skip.
	return nil
}

func NewRedisOpt(addr string) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: addr}
}

func (e *Enqueuer) EnqueueCaseRun(ctx context.Context, caseRunID uuid.UUID) error {
	payload, err := json.Marshal(ExecuteCaseRunPayload{CaseRunID: caseRunID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(TypeExecuteCaseRun, payload, asynq.TaskID(caseRunID.String()))
	_, err = e.client.EnqueueContext(ctx, task,
		asynq.MaxRetry(e.maxRetries),
		asynq.Queue(QueueDefault),
	)
	if err != nil {
		return fmt.Errorf("enqueue case run %s: %w", caseRunID, err)
	}
	return nil
}

func ParseExecutePayload(t *asynq.Task) (ExecuteCaseRunPayload, error) {
	var p ExecuteCaseRunPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return p, err
	}
	if p.CaseRunID == uuid.Nil {
		return p, fmt.Errorf("missing case_run_id")
	}
	return p, nil
}
