package domain

// RunStatus is the lifecycle state of an evaluation_run.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// CaseRunStatus is the lifecycle state of a case_run.
type CaseRunStatus string

const (
	CasePending   CaseRunStatus = "pending"
	CaseRunning   CaseRunStatus = "running"
	CaseSucceeded CaseRunStatus = "succeeded"
	CaseFailed    CaseRunStatus = "failed"
	CaseError     CaseRunStatus = "error"
	CaseCancelled CaseRunStatus = "cancelled"
)

// IsTerminal reports whether a case_run status is final.
func (s CaseRunStatus) IsTerminal() bool {
	switch s {
	case CaseSucceeded, CaseFailed, CaseError, CaseCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionRun returns true if moving from -> to is allowed.
func CanTransitionRun(from, to RunStatus) bool {
	switch from {
	case RunPending:
		return to == RunRunning || to == RunCancelled || to == RunFailed
	case RunRunning:
		return to == RunCompleted || to == RunFailed || to == RunCancelled
	default:
		return false
	}
}

// CanTransitionCase returns true if moving from -> to is allowed.
func CanTransitionCase(from, to CaseRunStatus) bool {
	switch from {
	case CasePending:
		return to == CaseRunning || to == CaseCancelled
	case CaseRunning:
		return to == CaseSucceeded || to == CaseFailed || to == CaseError || to == CaseCancelled
	default:
		return false
	}
}

// AggregateRunStatus computes the parent run status from case_run statuses.
// Non-terminal cases keep the run in running (or pending if none started).
func AggregateRunStatus(cases []CaseRunStatus) RunStatus {
	if len(cases) == 0 {
		return RunFailed
	}

	allPending := true
	allTerminal := true
	anyFailed := false
	anyCancelled := false
	anyRunning := false

	for _, s := range cases {
		if s != CasePending {
			allPending = false
		}
		if !s.IsTerminal() {
			allTerminal = false
		}
		if s == CaseRunning {
			anyRunning = true
		}
		if s == CaseFailed || s == CaseError {
			anyFailed = true
		}
		if s == CaseCancelled {
			anyCancelled = true
		}
	}

	if allPending {
		return RunPending
	}
	if !allTerminal || anyRunning {
		return RunRunning
	}
	if anyCancelled && !anyFailed {
		// All terminal; if any cancelled and none failed/error, treat as cancelled
		// (unless all succeeded — handled below).
		allSucceeded := true
		for _, s := range cases {
			if s != CaseSucceeded {
				allSucceeded = false
				break
			}
		}
		if allSucceeded {
			return RunCompleted
		}
		// Mix of succeeded + cancelled, or all cancelled
		allCancelled := true
		for _, s := range cases {
			if s != CaseCancelled {
				allCancelled = false
				break
			}
		}
		if allCancelled {
			return RunCancelled
		}
		// Partial cancel with successes and no failures → cancelled
		return RunCancelled
	}
	if anyFailed {
		return RunFailed
	}
	allSucceeded := true
	for _, s := range cases {
		if s != CaseSucceeded {
			allSucceeded = false
			break
		}
	}
	if allSucceeded {
		return RunCompleted
	}
	return RunFailed
}
