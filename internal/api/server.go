package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/cu7ious/the-judge/internal/domain"
	"github.com/cu7ious/the-judge/internal/jobs"
	"github.com/cu7ious/the-judge/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Server hosts the REST API.
type Server struct {
	Store    *store.Store
	Enqueuer *jobs.Enqueuer
	Redis    *redis.Client
	Log      *slog.Logger
}

func NewServer(st *store.Store, enq *jobs.Enqueuer, rdb *redis.Client, log *slog.Logger) *Server {
	return &Server{Store: st, Enqueuer: enq, Redis: rdb, Log: log}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(s.logRequests)

	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/suites", s.handleCreateSuite)
		r.Get("/suites", s.handleListSuites)
		r.Get("/suites/{id}", s.handleGetSuite)
		r.Post("/suites/{id}/cases", s.handleCreateCase)

		r.Post("/runs", s.handleCreateRun)
		r.Get("/runs/{id}", s.handleGetRun)
		r.Get("/runs/{id}/results", s.handleGetRunResults)
		r.Post("/runs/{id}/cancel", s.handleCancelRun)
	})

	return r
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.Log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.Store.Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "postgres not ready")
		return
	}
	if s.Redis != nil {
		if err := s.Redis.Ping(ctx).Err(); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "redis not ready")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type createSuiteRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreateSuite(w http.ResponseWriter, r *http.Request) {
	var req createSuiteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	suite, err := s.Store.CreateSuite(r.Context(), req.Name, req.Description)
	if err != nil {
		s.Log.Error("create suite", "err", err)
		writeErr(w, http.StatusInternalServerError, "failed to create suite")
		return
	}
	writeJSON(w, http.StatusCreated, suite)
}

func (s *Server) handleListSuites(w http.ResponseWriter, r *http.Request) {
	suites, err := s.Store.ListSuites(r.Context(), 50)
	if err != nil {
		s.Log.Error("list suites", "err", err)
		writeErr(w, http.StatusInternalServerError, "failed to list suites")
		return
	}
	if suites == nil {
		suites = []domain.Suite{}
	}
	writeJSON(w, http.StatusOK, suites)
}

func (s *Server) handleGetSuite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid suite id")
		return
	}
	suite, err := s.Store.GetSuite(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "suite not found")
		return
	}
	if err != nil {
		s.Log.Error("get suite", "err", err)
		writeErr(w, http.StatusInternalServerError, "failed to get suite")
		return
	}
	writeJSON(w, http.StatusOK, suite)
}

type createCaseRequest struct {
	Name       string          `json:"name"`
	Prompt     string          `json:"prompt"`
	Expected   json.RawMessage `json:"expected"`
	Validators json.RawMessage `json:"validators"`
	TimeoutMs  int             `json:"timeout_ms"`
	Position   int             `json:"position"`
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	suiteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid suite id")
		return
	}
	if _, err := s.Store.GetSuite(r.Context(), suiteID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "suite not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load suite")
		return
	}

	var req createCaseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.Prompt == "" {
		writeErr(w, http.StatusBadRequest, "name and prompt are required")
		return
	}

	tc, err := s.Store.CreateTestCase(r.Context(), domain.TestCase{
		SuiteID:    suiteID,
		Name:       req.Name,
		Prompt:     req.Prompt,
		Expected:   req.Expected,
		Validators: req.Validators,
		TimeoutMs:  req.TimeoutMs,
		Position:   req.Position,
	})
	if err != nil {
		s.Log.Error("create case", "err", err)
		writeErr(w, http.StatusInternalServerError, "failed to create test case")
		return
	}
	writeJSON(w, http.StatusCreated, tc)
}

type createRunRequest struct {
	SuiteID uuid.UUID         `json:"suite_id"`
	Models  []domain.ModelRef `json:"models"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.SuiteID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "suite_id is required")
		return
	}
	if len(req.Models) == 0 {
		writeErr(w, http.StatusBadRequest, "models is required")
		return
	}
	for _, m := range req.Models {
		if m.Provider == "" || m.Model == "" {
			writeErr(w, http.StatusBadRequest, "each model needs provider and model")
			return
		}
	}

	suite, err := s.Store.GetSuite(r.Context(), req.SuiteID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "suite not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load suite")
		return
	}
	if len(suite.Cases) == 0 {
		writeErr(w, http.StatusBadRequest, "suite has no test cases")
		return
	}

	var caseRuns []domain.CaseRun
	for _, tc := range suite.Cases {
		for _, m := range req.Models {
			caseRuns = append(caseRuns, domain.CaseRun{
				TestCaseID: tc.ID,
				Provider:   m.Provider,
				Model:      m.Model,
			})
		}
	}

	run, err := s.Store.CreateRun(r.Context(), req.SuiteID, req.Models, caseRuns)
	if err != nil {
		s.Log.Error("create run", "err", err)
		writeErr(w, http.StatusInternalServerError, "failed to create run")
		return
	}

	// Reload case runs with IDs
	created, err := s.Store.ListCaseRuns(r.Context(), run.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list case runs")
		return
	}

	for _, cr := range created {
		if err := s.Enqueuer.EnqueueCaseRun(r.Context(), cr.ID); err != nil {
			s.Log.Error("enqueue failed", "err", err, "run_id", run.ID)
			_ = s.Store.MarkRunFailedEnqueue(r.Context(), run.ID, "enqueue failed: "+err.Error())
			writeErr(w, http.StatusServiceUnavailable, "failed to enqueue jobs; run marked failed")
			return
		}
	}

	summary, err := s.Store.GetRunSummary(r.Context(), run.ID)
	if err != nil {
		writeJSON(w, http.StatusCreated, run)
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	summary, err := s.Store.GetRunSummary(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to get run")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleGetRunResults(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	run, err := s.Store.GetRun(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to get run")
		return
	}
	results, err := s.Store.GetRunResults(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to get results")
		return
	}
	if results == nil {
		results = []domain.CaseRunWithValidations{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run":     run,
		"results": results,
	})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	err = s.Store.CancelRun(r.Context(), id)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "run cannot be cancelled in its current state")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to cancel run")
		return
	}
	summary, _ := s.Store.GetRunSummary(r.Context(), id)
	writeJSON(w, http.StatusOK, summary)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
