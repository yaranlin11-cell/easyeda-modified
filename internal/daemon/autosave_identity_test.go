package daemon

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhoushoujianwork/easyeda-agent/internal/protocol"
)

func TestAutosaverTargetsRemainIndependentInOneWindow(t *testing.T) {
	a := newAutosaver(time.Hour, func(autosaveTicket) { t.Error("unexpected timer") })
	defer a.stop()
	base := testAutosaveTarget("w1", "schematic")
	otherDoc, otherProject, otherType := base, base, base
	otherDoc.Context.DocumentUUID = "doc-other"
	otherProject.Context.ProjectUUID = "project-other"
	otherType.Context.DocumentType, otherType.SaveAction = "pcb", "pcb.save"
	for _, target := range []autosaveTarget{base, otherDoc, otherProject, otherType} {
		a.schedule(target)
	}
	if len(a.snapshot()) != 4 {
		t.Fatalf("documents/projects/types in one window were coalesced: %+v", a.snapshot())
	}
	for _, target := range []autosaveTarget{base, otherDoc, otherProject, otherType} {
		if !a.current(testAutosaveTicket(t, a, target)) {
			t.Fatalf("lost independent target: %+v", target)
		}
	}
}

func TestMaybeAutosaveFreezesResponseIdentityAndRefusesUnknownTarget(t *testing.T) {
	for _, field := range []string{"complete", "project", "document", "type", "mismatched type"} {
		t.Run(field, func(t *testing.T) {
			s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
			defer s.autosave.stop()
			target := testAutosaveTarget("w1", "pcb")
			resp := testAutosaveResponse(target)
			switch field {
			case "project":
				resp.MutationContext.ProjectUUID = ""
			case "document":
				resp.MutationContext.DocumentUUID = ""
			case "type":
				resp.MutationContext.DocumentType = ""
			case "mismatched type":
				resp.MutationContext.DocumentType = "schematic"
			}
			req := &protocol.Request{Envelope: protocol.Envelope{WindowID: "w1"}, Action: "pcb.component.modify"}
			s.maybeAutosave(req, resp, time.Now())
			if field == "complete" {
				ticket := testAutosaveTicket(t, s.autosave, target)
				resp.Context.ProjectUUID, resp.Context.DocumentUUID = "later-project", "later-document"
				resp.MutationContext.ProjectUUID, resp.MutationContext.DocumentUUID = "later-project", "later-document"
				if ticket.Context != target.Context || len(resp.Warnings) != 0 {
					t.Fatalf("target aliased a later response identity or was rejected: %+v", ticket)
				}
			} else {
				statuses := s.autosave.snapshot()
				if len(s.autosave.timers) != 0 || len(statuses) != 1 || statuses[0].State != "unavailable" || statuses[0].Saved || len(resp.Warnings) != 1 {
					t.Fatalf("unproven identity armed a save or hid incompleteness: timers=%v statuses=%+v warnings=%v", s.autosave.timers, statuses, resp.Warnings)
				}
			}
		})
	}
}

func TestExplicitSaveCancelsOnlyConfirmedOlderTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     map[string]any
		ok, cancel bool
	}{
		{"confirmed", map[string]any{"saved": true}, true, true},
		{"false", map[string]any{"saved": false}, true, false},
		{"missing", map[string]any{}, true, false},
		{"null", map[string]any{"saved": nil}, true, false},
		{"string", map[string]any{"saved": "true"}, true, false},
		{"partial", map[string]any{"saved": true, "partial": true}, true, false},
		{"outer failure", map[string]any{"saved": true}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
			defer s.autosave.stop()
			target := testAutosaveTarget("w1", "pcb")
			s.autosave.schedule(target)
			ticket := testAutosaveTicket(t, s.autosave, target)
			started := time.Now()
			resp := testAutosaveResponse(target)
			resp.OK, resp.Result = tc.ok, tc.result
			s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: target.SaveAction}, resp, started)
			if s.autosave.current(ticket) == tc.cancel {
				t.Fatalf("cancel=%t but current=%t", tc.cancel, s.autosave.current(ticket))
			}
			status := s.autosave.snapshot()[0]
			if tc.cancel && (status.State != "saved" || !status.Saved) || !tc.cancel && status.State != "pending" {
				t.Fatalf("wrong explicit-save evidence: %+v", status)
			}
		})
	}

	s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
	defer s.autosave.stop()
	target := testAutosaveTarget("w1", "pcb")
	s.autosave.schedule(target)
	started := time.Now()
	s.autosave.schedule(target) // a mutation completed while explicit save ran
	newTicket := testAutosaveTicket(t, s.autosave, target)
	resp := testAutosaveResponse(target)
	resp.Result = map[string]any{"saved": true}
	req := &protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: target.SaveAction}
	s.maybeAutosave(req, resp, started)
	if !s.autosave.current(newTicket) || s.autosave.snapshot()[0].State != "pending" {
		t.Fatal("explicit save canceled a mutation newer than the request")
	}
	other := target
	other.Context.DocumentUUID = "different-document"
	otherResp := testAutosaveResponse(other)
	otherResp.Result = map[string]any{"saved": true}
	s.maybeAutosave(req, otherResp, time.Now())
	if !s.autosave.current(newTicket) {
		t.Fatal("a save of another document canceled the pending document")
	}
}

func TestAutosaverFiredOldCallbackCannotClearOrRearmNewGeneration(t *testing.T) {
	entered := make(chan autosaveTicket, 1)
	release, done := make(chan struct{}), make(chan struct{})
	var a *autosaver
	a = newAutosaver(10*time.Millisecond, func(ticket autosaveTicket) {
		entered <- ticket
		<-release
		a.finish(ticket, "saved", "")
		close(done)
	})
	defer a.stop()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	target := testAutosaveTarget("w1", "pcb")
	a.schedule(target)
	var old autosaveTicket
	select {
	case old = <-entered:
	case <-time.After(time.Second):
		t.Fatal("timer did not enter its callback")
	}
	a.mu.Lock()
	a.debounce = time.Hour
	a.mu.Unlock()
	a.schedule(target)
	current := testAutosaveTicket(t, a, target)
	if old.generation == current.generation || a.current(old) {
		t.Fatal("mutation reused a fired generation")
	}
	if !a.deferBusy(old, time.Now()) || !a.current(current) {
		t.Fatal("stale busy callback altered the new generation")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old callback did not finish")
	}
	if !a.current(current) || a.snapshot()[0].State != "pending" {
		t.Fatal("old completion canceled or marked a newer mutation saved")
	}
}

func TestAutosaverNewGenerationPreservesDeferralClock(t *testing.T) {
	a := newAutosaver(time.Hour, func(autosaveTicket) {})
	defer a.stop()
	target := testAutosaveTarget("w1", "pcb")
	a.schedule(target)
	first := testAutosaveTicket(t, a, target)
	now := time.Now()
	if !a.deferBusy(first, now.Add(-autosaveMaxDefer-time.Second)) {
		t.Fatal("initial busy callback should defer")
	}
	a.schedule(target)
	second := testAutosaveTicket(t, a, target)
	if a.deferBusy(second, now) {
		t.Fatal("new mutations reset the cap and can postpone autosave forever")
	}
}

func TestAutosaveRequestCarriesFrozenIdentity(t *testing.T) {
	for _, tab := range []string{"tab-1", ""} {
		target := testAutosaveTarget("w1", "pcb")
		target.Context.TabID = tab
		req := autosaveRequest(autosaveTicket{autosaveTarget: target, generation: 7}, "request-1")
		want := map[string]any{"projectUuid": target.Context.ProjectUUID, "documentUuid": target.Context.DocumentUUID, "documentType": "pcb"}
		if tab != "" {
			want["tabId"] = tab
		}
		if req.WindowID != target.WindowID || req.Action != "pcb.save" || req.ID != "request-1" || !reflect.DeepEqual(req.Payload["expectedContext"], want) {
			t.Fatalf("save can drift to the foreground document: %+v", req)
		}
	}
}

func TestMaybeAutosaveFailedAndReadRequestsDoNotArm(t *testing.T) {
	s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
	defer s.autosave.stop()
	resp := testAutosaveResponse(testAutosaveTarget("w1", "pcb"))
	for _, action := range []string{"pcb.components.list", "pcb.save", "unknown.action"} {
		s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: "w1"}, Action: action}, resp, time.Now())
	}
	resp.OK = false
	s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: "w1"}, Action: "pcb.component.modify"}, resp, time.Now())
	s.maybeAutosave(nil, resp, time.Now())
	s.maybeAutosave(&protocol.Request{Action: "pcb.component.modify"}, nil, time.Now())
	if len(s.autosave.timers) != 0 {
		t.Fatal("failed or non-mutating request armed autosave")
	}
	for _, status := range s.autosave.snapshot() {
		if strings.Contains(status.State, "pending") {
			t.Fatalf("non-mutating request created pending evidence: %+v", status)
		}
	}
}

func TestMaybeAutosaveUsesCertifiedIdentityInsteadOfActiveDiagnostic(t *testing.T) {
	s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
	defer s.autosave.stop()
	target := testAutosaveTarget("w1", "pcb")
	resp := testAutosaveResponse(target)
	resp.Context.DocumentUUID = "post-action-active-document"
	s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: "pcb.component.modify"}, resp, time.Now())
	ticket := testAutosaveTicket(t, s.autosave, target)
	if ticket.Context.DocumentUUID != target.Context.DocumentUUID || len(resp.Warnings) != 0 {
		t.Fatalf("post-action diagnostic overrode certified target: %+v", ticket)
	}
}

func TestMutationAndExplicitSaveDoNotTrustLegacyOrUncertifiedIdentity(t *testing.T) {
	for _, action := range []string{"pcb.component.modify", "pcb.save"} {
		for _, mode := range []string{"legacy", "false", "missing certified context", "missing certified document"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
				defer s.autosave.stop()
				target, other := testAutosaveTarget("w1", "pcb"), testAutosaveTarget("w1", "pcb")
				other.Context.DocumentUUID = "other-document"
				s.autosave.schedule(other)
				otherTicket := testAutosaveTicket(t, s.autosave, other)
				var originalTicket autosaveTicket
				if action == "pcb.save" {
					s.autosave.schedule(target)
					originalTicket = testAutosaveTicket(t, s.autosave, target)
				}
				resp := testAutosaveResponse(target)
				resp.Result = map[string]any{"saved": true}
				switch mode {
				case "legacy":
					resp.MutationContextAvailable, resp.MutationContext = nil, nil
				case "false":
					available := false
					resp.MutationContextAvailable = &available
					resp.MutationContextError = "handler-boundary documentUuid changed"
				case "missing certified context":
					resp.MutationContext = nil
				case "missing certified document":
					resp.MutationContext.DocumentUUID = ""
				}
				s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: action}, resp, time.Now())
				if !resp.OK || resp.Result["saved"] != true || len(resp.Warnings) != 1 {
					t.Fatalf("native result was overwritten or missing warning: %+v", resp)
				}
				if !s.autosave.current(otherTicket) || action == "pcb.save" && !s.autosave.current(originalTicket) {
					t.Fatal("unproven save canceled a pending timer")
				}
				wantTimers := 1
				if action == "pcb.save" {
					wantTimers = 2
				}
				if len(s.autosave.timers) != wantTimers {
					t.Fatalf("unproven mutation armed autosave: %v", s.autosave.timers)
				}
			})
		}
	}
}

func TestDocumentSourceRoundtripAutosaveUsesCertifiedDocumentType(t *testing.T) {
	for _, docType := range []string{"schematic", "pcb"} {
		for _, mode := range []string{"certified", "dry-run", "legacy", "drift", "missing document"} {
			t.Run(docType+"/"+mode, func(t *testing.T) {
				s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
				defer s.autosave.stop()
				target := testAutosaveTarget("w1", docType)
				resp := testAutosaveResponse(target)
				req := &protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: "document.source.roundtrip", Payload: map[string]any{}}
				switch mode {
				case "dry-run":
					req.Payload["dryRun"] = true
				case "legacy":
					resp.MutationContextAvailable = nil
				case "drift":
					available := false
					resp.MutationContextAvailable = &available
				case "missing document":
					resp.MutationContext.DocumentUUID = ""
				}
				s.maybeAutosave(req, resp, time.Now())
				if mode == "certified" {
					ticket := testAutosaveTicket(t, s.autosave, target)
					if ticket.SaveAction != docType+".save" {
						t.Fatalf("wrong source save target: %+v", ticket)
					}
				} else if len(s.autosave.timers) != 0 {
					t.Fatal("unproven or preview source roundtrip armed autosave")
				}
				if mode == "dry-run" && len(s.autosave.snapshot()) != 0 {
					t.Fatal("preview source created autosave evidence")
				}
				if mode != "certified" && mode != "dry-run" && len(resp.Warnings) != 1 {
					t.Fatalf("unproven source missing warning: %+v", resp)
				}
			})
		}
	}
	s := &Server{autosave: newAutosaver(time.Hour, func(autosaveTicket) {})}
	defer s.autosave.stop()
	for _, action := range []string{"project.open", "debug.exec_js", "document.source.get", "view.region", "pcb.view.filter.get"} {
		s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: "w1"}, Action: action}, testAutosaveResponse(testAutosaveTarget("w1", "pcb")), time.Now())
	}
	if len(s.autosave.timers) != 0 {
		t.Fatal("generic domain action inferred a document save")
	}
}
