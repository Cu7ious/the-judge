package runner_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/cu7ious/the-judge/internal/domain"
	"github.com/cu7ious/the-judge/internal/migrate"
	"github.com/cu7ious/the-judge/internal/obs"
	"github.com/cu7ious/the-judge/internal/provider"
	"github.com/cu7ious/the-judge/internal/runner"
	"github.com/cu7ious/the-judge/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestExecuteHappyPath(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()

	suite, err := st.CreateSuite(ctx, "runner-suite", "")
	if err != nil {
		t.Fatal(err)
	}
	tc, err := st.CreateTestCase(ctx, domain.TestCase{
		SuiteID:    suite.ID,
		Name:       "hello",
		Prompt:     "say hello",
		Validators: []byte(`[{"type":"contains","value":"hello"}]`),
		TimeoutMs:  5000,
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := st.CreateRun(ctx, suite.ID, []domain.ModelRef{{Provider: "fake", Model: "m1"}}, []domain.CaseRun{
		{TestCaseID: tc.ID, Provider: "fake", Model: "m1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases, err := st.ListCaseRuns(ctx, run.ID)
	if err != nil || len(cases) != 1 {
		t.Fatalf("case runs: %v %v", cases, err)
	}

	fake := &provider.Fake{Response: "hello from model"}
	r := &runner.Runner{
		Store:     st,
		Providers: provider.Registry{"fake": fake},
		Log:       obs.NewLogger("error"),
	}

	if err := r.Execute(ctx, cases[0].ID); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetCaseRun(ctx, cases[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.CaseSucceeded {
		t.Fatalf("status=%s err=%v", got.Status, got.Error)
	}

	summary, err := st.GetRunSummary(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != domain.RunCompleted {
		t.Fatalf("run status=%s", summary.Status)
	}
}

func TestExecuteValidationFailureNoRetrySemantics(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()

	suite, _ := st.CreateSuite(ctx, "fail-suite", "")
	tc, _ := st.CreateTestCase(ctx, domain.TestCase{
		SuiteID:    suite.ID,
		Name:       "exact",
		Prompt:     "x",
		Validators: []byte(`[{"type":"exact_match","value":"YES"}]`),
		TimeoutMs:  5000,
	})
	run, _ := st.CreateRun(ctx, suite.ID, []domain.ModelRef{{Provider: "fake", Model: "m"}}, []domain.CaseRun{
		{TestCaseID: tc.ID, Provider: "fake", Model: "m"},
	})
	cases, _ := st.ListCaseRuns(ctx, run.ID)

	r := &runner.Runner{
		Store:     st,
		Providers: provider.Registry{"fake": &provider.Fake{Response: "NO"}},
		Log:       obs.NewLogger("error"),
	}
	if err := r.Execute(ctx, cases[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCaseRun(ctx, cases[0].ID)
	if got.Status != domain.CaseFailed {
		t.Fatalf("want failed got %s", got.Status)
	}
}

func TestExecuteIdempotent(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	suite, _ := st.CreateSuite(ctx, "idem", "")
	tc, _ := st.CreateTestCase(ctx, domain.TestCase{
		SuiteID: suite.ID, Name: "c", Prompt: "p",
		Validators: []byte(`[]`), TimeoutMs: 5000,
	})
	run, _ := st.CreateRun(ctx, suite.ID, []domain.ModelRef{{Provider: "fake", Model: "m"}}, []domain.CaseRun{
		{TestCaseID: tc.ID, Provider: "fake", Model: "m"},
	})
	cases, _ := st.ListCaseRuns(ctx, run.ID)
	fake := &provider.Fake{Response: "ok"}
	r := &runner.Runner{
		Store: st, Providers: provider.Registry{"fake": fake}, Log: obs.NewLogger("error"),
	}
	if err := r.Execute(ctx, cases[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Execute(ctx, cases[0].ID); err != nil {
		t.Fatal(err)
	}
	if fake.Calls != 1 {
		t.Fatalf("expected 1 call, got %d", fake.Calls)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://judge:judge@localhost:5432/judge?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(pool.Close)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrationsDir := "migrations"
	for _, c := range []string{"migrations", "../migrations", "../../migrations"} {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			migrationsDir = c
			break
		}
	}
	if err := migrate.Up(ctx, db, migrationsDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := store.New(pool)
	_, _ = pool.Exec(context.Background(), `
		TRUNCATE validation_results, case_runs, evaluation_runs, test_cases, suites CASCADE
	`)
	return st
}
