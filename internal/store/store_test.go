package store

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/cu7ious/the-judge/internal/domain"
	"github.com/cu7ious/the-judge/internal/migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSuiteCRUD(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	suite, err := store.CreateSuite(ctx, "smoke", "desc")
	if err != nil {
		t.Fatalf("CreateSuite: %v", err)
	}
	if suite.Name != "smoke" {
		t.Fatalf("name=%q", suite.Name)
	}

	got, err := store.GetSuite(ctx, suite.ID)
	if err != nil {
		t.Fatalf("GetSuite: %v", err)
	}
	if got.ID != suite.ID {
		t.Fatalf("id mismatch")
	}

	tc, err := store.CreateTestCase(ctx, domain.TestCase{
		SuiteID:    suite.ID,
		Name:       "case-1",
		Prompt:     "Say hello",
		TimeoutMs:  5000,
		Validators: []byte(`[{"type":"contains","value":"hello"}]`),
	})
	if err != nil {
		t.Fatalf("CreateTestCase: %v", err)
	}

	got, err = store.GetSuite(ctx, suite.ID)
	if err != nil {
		t.Fatalf("GetSuite with cases: %v", err)
	}
	if len(got.Cases) != 1 || got.Cases[0].ID != tc.ID {
		t.Fatalf("cases=%v", got.Cases)
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://judge:judge@localhost:5432/judge?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close(); pool.Close() })

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = findMigrations()
	}
	if err := migrate.Up(ctx, db, migrationsDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, _ = pool.Exec(ctx, `
		TRUNCATE validation_results, case_runs, evaluation_runs, test_cases, suites CASCADE
	`)

	return New(pool)
}

func findMigrations() string {
	candidates := []string{"migrations", "../migrations", "../../migrations"}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return "migrations"
}
