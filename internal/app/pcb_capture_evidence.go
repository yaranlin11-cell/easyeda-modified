package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Capture identity is observational provenance, never a claim of saved state.
// Per-response boundary checks detect observed switches; the SDK cannot prove
// that an A→B→A tab switch did not happen inside one handler.
type pcbCaptureEvidence struct {
	WindowID         string              `json:"windowId,omitempty"`
	ProjectUUID      string              `json:"projectUuid,omitempty"`
	ProjectName      string              `json:"projectName,omitempty"`
	DocumentUUID     string              `json:"documentUuid,omitempty"`
	DocumentType     string              `json:"documentType,omitempty"`
	TabID            string              `json:"tabId,omitempty"`
	StartedAt        string              `json:"startedAt"`
	CompletedAt      string              `json:"completedAt,omitempty"`
	Consistency      string              `json:"consistency"`
	IdentityVerified bool                `json:"identityVerified"`
	Missing          []string            `json:"missing,omitempty"`
	Requests         []pcbCaptureRequest `json:"requests"`
	mismatch         error
}

type pcbCaptureRequest struct {
	Action            string         `json:"action"`
	RequestID         string         `json:"requestId,omitempty"`
	WindowID          string         `json:"windowId,omitempty"`
	StartedAt         string         `json:"startedAt"`
	CompletedAt       string         `json:"completedAt"`
	ResponseCreatedAt string         `json:"responseCreatedAt,omitempty"`
	ResultSHA256      string         `json:"resultSha256,omitempty"`
	Context           *actionContext `json:"context,omitempty"`
	Sequence          schSeqCounters `json:"sequence"`
	Status            string         `json:"status"`
	Error             string         `json:"error,omitempty"`
}

func newPCBCaptureEvidence() *pcbCaptureEvidence {
	return &pcbCaptureEvidence{StartedAt: time.Now().UTC().Format(time.RFC3339Nano), IdentityVerified: true, Consistency: "sequential-response-boundaries", Requests: []pcbCaptureRequest{}}
}

func (c *pcbCaptureEvidence) missingEvidence(message string) {
	c.IdentityVerified = false
	for _, existing := range c.Missing {
		if existing == message {
			return
		}
	}
	c.Missing = append(c.Missing, message)
}

func (c *pcbCaptureEvidence) observe(action string, started time.Time, res *actionResult, err error) error {
	r := pcbCaptureRequest{Action: action, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "available"}
	defer func() { c.Requests = append(c.Requests, r); c.CompletedAt = r.CompletedAt }()
	if res == nil {
		r.Status = "unknown"
		if err != nil {
			r.Error = err.Error()
		}
		c.missingEvidence(action + ": no response identity")
		return err
	}
	r.RequestID, r.WindowID, r.Context = res.ID, res.WindowID, res.Context
	r.Sequence = res.Seq
	if res.Seq.Unordered {
		c.missingEvidence(action + ": unordered response boundary")
	}
	if !res.CreatedAt.IsZero() {
		r.ResponseCreatedAt = res.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if raw, e := json.Marshal(res.Result); e == nil {
		r.ResultSHA256 = sha256Hex(raw)
	}
	if r.RequestID == "" {
		c.missingEvidence(action + ": request ID missing")
	}
	if r.ResponseCreatedAt == "" {
		c.missingEvidence(action + ": response creation time missing")
	}
	if res.WindowID == "" {
		c.missingEvidence(action + ": window identity missing")
	}
	same := func(label string, saved *string, observed string) {
		if observed == "" {
			c.missingEvidence(action + ": " + label + " missing")
			return
		}
		if *saved == "" {
			*saved = observed
			return
		}
		if *saved != observed && c.mismatch == nil {
			c.mismatch = fmt.Errorf("PCB capture identity drift during %s: %s changed from %q to %q", action, label, *saved, observed)
		}
	}
	same("windowId", &c.WindowID, res.WindowID)
	if res.Context == nil {
		c.missingEvidence(action + ": context missing")
	} else {
		same("projectUuid", &c.ProjectUUID, res.Context.ProjectUUID)
		same("documentUuid", &c.DocumentUUID, res.Context.DocumentUUID)
		same("documentType", &c.DocumentType, res.Context.DocumentType)
		same("tabId", &c.TabID, res.Context.TabID)
		if c.ProjectName == "" {
			c.ProjectName = res.Context.ProjectName
		}
		if res.Context.DocumentType != "" && !strings.EqualFold(res.Context.DocumentType, "pcb") && c.mismatch == nil {
			c.mismatch = fmt.Errorf("PCB capture received %s context during %s", res.Context.DocumentType, action)
		}
	}
	if c.mismatch != nil {
		c.IdentityVerified = false
		r.Status, r.Error = "identity-mismatch", c.mismatch.Error()
		return c.mismatch
	}
	if err != nil {
		r.Status, r.Error = "unknown", err.Error()
	}
	return err
}

func (c *pcbCaptureEvidence) read(cfg *appConfig, action, window string, payload any) (*actionResult, error) {
	if c.mismatch != nil {
		return nil, c.mismatch
	}
	started := time.Now()
	res, err := requestAction(cfg, action, window, payload)
	if window != "" && res != nil && res.WindowID != "" && res.WindowID != window {
		c.mismatch = fmt.Errorf("PCB capture response window %q differs from requested window %q during %s", res.WindowID, window, action)
	}
	if checkErr := c.observe(action, started, res, err); checkErr != nil {
		return res, checkErr
	}
	return res, nil
}
