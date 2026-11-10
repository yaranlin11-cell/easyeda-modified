package app

import (
	"fmt"
	"time"
)

// Host times describe this CLI's observations, not timestamps claimed by EDA.
type documentReloadStep struct {
	Stage             string         `json:"stage"`
	Action            string         `json:"action,omitempty"`
	StartedAt         string         `json:"startedAt"`
	CompletedAt       string         `json:"completedAt"`
	RequestID         string         `json:"requestId,omitempty"`
	ResponseCreatedAt *time.Time     `json:"responseCreatedAt,omitempty"`
	Context           *actionContext `json:"context,omitempty"`
	OK                bool           `json:"ok"`
	Error             string         `json:"error,omitempty"`
}

type documentReloadReceipt struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	Status         string                    `json:"status"`
	WindowID       string                    `json:"windowId"`
	Target         string                    `json:"target"`
	DocumentType   string                    `json:"documentType"`
	Saved          bool                      `json:"saved"`
	Closed         bool                      `json:"closed"`
	Reloaded       string                    `json:"reloaded"`
	ActiveRestored string                    `json:"activeRestored"`
	StartedAt      string                    `json:"startedAt"`
	CompletedAt    string                    `json:"completedAt"`
	TimeSource     string                    `json:"timeSource"`
	FailedStage    string                    `json:"failedStage,omitempty"`
	Error          string                    `json:"error,omitempty"`
	TargetContext  *actionContext            `json:"targetContext,omitempty"`
	FinalContext   *actionContext            `json:"finalContext,omitempty"`
	Readiness      *documentReadyObservation `json:"readiness,omitempty"`
	Steps          []documentReloadStep      `json:"steps"`
}

func reloadNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (r *documentReloadReceipt) request(cfg *appConfig, stage, action, win string, payload any, timeout time.Duration) (*actionResult, error) {
	step := documentReloadStep{Stage: stage, Action: action, StartedAt: reloadNow()}
	res, err := requestActionOnce(cfg, action, win, payload, timeout)
	step.CompletedAt = reloadNow()
	if res != nil {
		step.RequestID, step.Context = res.ID, res.Context
		if !res.CreatedAt.IsZero() {
			stamp := res.CreatedAt
			step.ResponseCreatedAt = &stamp
		}
		if res.WindowID != "" && res.WindowID != win && err == nil {
			err = fmt.Errorf("reload response window differs from pinned window")
		}
	}
	step.OK = err == nil
	if err != nil {
		step.Error = err.Error()
	}
	r.Steps = append(r.Steps, step)
	return res, err
}

func (r *documentReloadReceipt) observeReady(cfg *appConfig, win, target string) (*documentReadyObservation, error) {
	step := documentReloadStep{Stage: "ready", StartedAt: reloadNow()}
	ready, err := verifyDocumentReady(cfg, win, target, documentReadyBudget)
	step.CompletedAt, step.OK = reloadNow(), err == nil
	if err != nil {
		step.Error = err.Error()
	}
	if ready != nil {
		r.Readiness, step.Context = ready, ready.Active
	}
	r.Steps = append(r.Steps, step)
	return ready, err
}

func reloadDocumentWithReceipt(cfg *appConfig, win, target, restore string) (r *documentReloadReceipt, err error) {
	r = &documentReloadReceipt{SchemaVersion: 1, Status: "failed", WindowID: win, Target: target,
		StartedAt: reloadNow(), TimeSource: "cli-host-observation", Steps: []documentReloadStep{}}
	defer func() {
		r.CompletedAt = reloadNow()
		if err != nil {
			r.Error = err.Error()
			if r.Saved || r.Closed {
				r.Status = "partial"
			}
			if len(r.Steps) > 0 {
				s := &r.Steps[len(r.Steps)-1]
				r.FailedStage = s.Stage
				s.OK = false
				s.Error = err.Error()
			}
		} else {
			r.Status = "passed"
		}
	}()
	// Never allow an inherited --doc selector to introduce unrecorded navigation.
	local := *cfg
	local.doc = ""
	r.DocumentType, err = reloadDocumentObserved(&local, win, target, r)
	if err != nil {
		return r, err
	}
	if restore == "" || restore == target {
		r.ActiveRestored = target
		return r, nil
	}
	if _, err = r.request(&local, "restore", "document.open", win, map[string]any{"uuid": restore}, defaultActionTimeout); err != nil {
		return r, err
	}
	var cur *actionResult
	cur, err = r.request(&local, "restore-verify", "document.current", win, nil, defaultActionTimeout)
	if err != nil {
		return r, err
	}
	if cur.Context == nil || cur.Context.DocumentUUID != restore || cur.Context.ProjectUUID != r.TargetContext.ProjectUUID || cur.Context.TabID == "" {
		return r, fmt.Errorf("reload succeeded but restoring previous active document was not verified")
	}
	r.ActiveRestored, r.FinalContext = restore, cur.Context
	return r, nil
}
