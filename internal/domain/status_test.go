package domain

import "testing"

func TestCanTransitionRun(t *testing.T) {
	tests := []struct {
		from, to RunStatus
		ok       bool
	}{
		{RunPending, RunRunning, true},
		{RunPending, RunCancelled, true},
		{RunRunning, RunCompleted, true},
		{RunRunning, RunFailed, true},
		{RunCompleted, RunRunning, false},
		{RunFailed, RunPending, false},
	}
	for _, tt := range tests {
		if got := CanTransitionRun(tt.from, tt.to); got != tt.ok {
			t.Errorf("CanTransitionRun(%s,%s)=%v want %v", tt.from, tt.to, got, tt.ok)
		}
	}
}

func TestAggregateRunStatus(t *testing.T) {
	tests := []struct {
		name  string
		cases []CaseRunStatus
		want  RunStatus
	}{
		{"all pending", []CaseRunStatus{CasePending, CasePending}, RunPending},
		{"mixed running", []CaseRunStatus{CasePending, CaseRunning}, RunRunning},
		{"all succeeded", []CaseRunStatus{CaseSucceeded, CaseSucceeded}, RunCompleted},
		{"one failed", []CaseRunStatus{CaseSucceeded, CaseFailed}, RunFailed},
		{"one error", []CaseRunStatus{CaseSucceeded, CaseError}, RunFailed},
		{"all cancelled", []CaseRunStatus{CaseCancelled, CaseCancelled}, RunCancelled},
		{"partial cancel no fail", []CaseRunStatus{CaseSucceeded, CaseCancelled}, RunCancelled},
		{"empty", nil, RunFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AggregateRunStatus(tt.cases); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}
