package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/zhoushoujianwork/easyeda-agent/internal/protocol"
)

// This fixture exercises the HTTP and WebSocket boundaries without touching a
// running daemon, the user's audit log, or an EDA document. Long debounce times
// keep saves deterministic; the tests invoke the captured timer ticket.
func newAutosaveDispatchFixture(t *testing.T, debounce time.Duration) (*Server, string) {
	t.Helper()
	s := New(Options{AutosaveDebounce: debounce, AuditDir: t.TempDir(), ArtifactDir: t.TempDir()})
	ctx, cancel := context.WithCancel(t.Context())
	s.connCtx = ctx
	srv := httptest.NewServer(s.routes(0))
	t.Cleanup(func() {
		s.autosave.stop()
		cancel()
		srv.Close()
	})
	return s, strings.TrimPrefix(srv.URL, "http://")
}

func autosaveDispatchPeer(t *testing.T, s *Server, base, window string, reply func(protocol.Request) protocol.Response) *websocket.Conn {
	t.Helper()
	c := dialConnector(t, base, window)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var req protocol.Request
			if err := wsjson.Read(s.connCtx, c, &req); err != nil {
				return
			}
			if req.Type != protocol.TypeRequest {
				continue
			}
			resp := reply(req)
			resp.Envelope = protocol.Envelope{ID: req.ID, Type: protocol.TypeResponse, Version: "v1", WindowID: window}
			if err := wsjson.Write(s.connCtx, c, resp); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = c.CloseNow()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("mock connector did not stop")
		}
	})
	waitForWindow(t, base, window)
	return c
}

func autosaveAuditEntries(t *testing.T, s *Server) []auditEntry {
	t.Helper()
	s.audit.mu.Lock()
	defer s.audit.mu.Unlock()
	paths, err := filepath.Glob(filepath.Join(s.audit.Dir(), "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []auditEntry
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var entry auditEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				_ = f.Close()
				t.Fatal(err)
			}
			entries = append(entries, entry)
		}
		err = scanner.Err()
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return entries
}

func requireAutosaveHealthState(t *testing.T, base string, target autosaveTarget, state string) AutosaveStatus {
	t.Helper()
	for _, status := range getHealth(t, base).Autosave {
		if status.key() == target.key() {
			if status.State != state || status.Saved != (state == "saved") || status.Context != target.Context || status.UpdatedAt.IsZero() {
				t.Fatalf("unexpected autosave evidence: %+v, want target=%+v state=%s", status, target, state)
			}
			return status
		}
	}
	t.Fatalf("target absent from /health autosave: %+v", target)
	return AutosaveStatus{}
}

func TestExplicitSaveHTTPPreservesNativeFailureAndPendingTimer(t *testing.T) {
	for _, docType := range []string{"schematic", "pcb"} {
		for _, tc := range []struct {
			name   string
			result map[string]any
			outer  bool
			code   string
		}{
			{"false", map[string]any{"saved": false, "reason": "host rejected", "writtenId": "original"}, true, "SAVE_UNCONFIRMED"},
			{"missing", map[string]any{"reason": "no save verdict"}, true, "SAVE_UNCONFIRMED"},
			{"null", map[string]any{"saved": nil}, true, "SAVE_UNCONFIRMED"},
			{"string", map[string]any{"saved": "true"}, true, "SAVE_UNCONFIRMED"},
			{"partial", map[string]any{"saved": true, "partial": true, "reason": "incomplete"}, true, "SAVE_UNCONFIRMED"},
			{"outer failure", map[string]any{"saved": false, "reason": "denied"}, false, "HOST_SAVE_FAILED"},
		} {
			t.Run(docType+"/"+tc.name, func(t *testing.T) {
				s, base := newAutosaveDispatchFixture(t, time.Hour)
				target := testAutosaveTarget("save-http", docType)
				var calls atomic.Int32
				autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
					calls.Add(1)
					resp := *testAutosaveResponse(target)
					resp.OK, resp.Result = tc.outer, tc.result
					if !tc.outer {
						resp.Error = &protocol.ErrorInfo{Code: tc.code, Message: "original host message", Detail: "original detail"}
					}
					return resp
				})
				s.autosave.schedule(target)
				ticket := testAutosaveTicket(t, s.autosave, target)
				resp := postAction(t, base, fmt.Sprintf(`{"action":%q,"windowId":%q}`, target.SaveAction, target.WindowID))
				if resp.OK || resp.Error == nil || resp.Error.Code != tc.code || !reflect.DeepEqual(resp.Result, tc.result) {
					t.Fatalf("native failure lost at HTTP boundary: %+v", resp)
				}
				if !tc.outer && (resp.Error.Message != "original host message" || resp.Error.Detail != "original detail") {
					t.Fatalf("host error overwritten: %+v", resp.Error)
				}
				if !s.autosave.current(ticket) || calls.Load() != 1 {
					t.Fatal("unconfirmed explicit save canceled its timer or retried")
				}
				requireAutosaveHealthState(t, base, target, "pending")
				entries := autosaveAuditEntries(t, s)
				if len(entries) != 1 || entries[0].OK || entries[0].ErrorCode != tc.code || !reflect.DeepEqual(entries[0].Result, tc.result) {
					t.Fatalf("native failure lost in audit: %+v", entries)
				}
			})
		}
	}
}

func TestExplicitSaveHTTPConfirmedCancelsOlderTimerWithoutRecursion(t *testing.T) {
	for _, docType := range []string{"schematic", "pcb"} {
		t.Run(docType, func(t *testing.T) {
			s, base := newAutosaveDispatchFixture(t, time.Hour)
			target := testAutosaveTarget("confirmed-http", docType)
			var calls atomic.Int32
			autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
				calls.Add(1)
				resp := *testAutosaveResponse(target)
				resp.Result = map[string]any{"saved": true}
				return resp
			})
			s.autosave.schedule(target)
			old := testAutosaveTicket(t, s.autosave, target)
			resp := postAction(t, base, fmt.Sprintf(`{"action":%q,"windowId":%q}`, target.SaveAction, target.WindowID))
			if !resp.OK || s.autosave.current(old) {
				t.Fatalf("confirmed save did not cancel older pending work: %+v", resp)
			}
			requireAutosaveHealthState(t, base, target, "saved")
			s.dispatchSave(old) // a previously captured callback must not resend
			if calls.Load() != 1 {
				t.Fatal("explicit save armed another save or replayed old callback")
			}
		})
	}
}

func TestExplicitSaveHTTPInFlightKeepsNewMutationPending(t *testing.T) {
	s, base := newAutosaveDispatchFixture(t, time.Hour)
	target := testAutosaveTarget("in-flight-explicit-save", "pcb")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
		entered <- struct{}{}
		select {
		case <-release:
		case <-s.connCtx.Done():
		}
		resp := *testAutosaveResponse(target)
		resp.Result = map[string]any{"saved": true}
		return resp
	})
	s.autosave.schedule(target)
	old := testAutosaveTicket(t, s.autosave, target)
	result := make(chan protocol.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Post("http://"+base+"/action", "application/json", strings.NewReader(`{"action":"pcb.save","windowId":"in-flight-explicit-save"}`))
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()
		var out protocol.Response
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			errCh <- err
			return
		}
		result <- out
	}()
	select {
	case <-entered:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("explicit save did not reach connector")
	}
	s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: "pcb.component.modify"}, testAutosaveResponse(target), time.Now())
	current := testAutosaveTicket(t, s.autosave, target)
	if current.generation == old.generation {
		t.Fatal("new mutation reused the old generation")
	}
	release <- struct{}{}
	select {
	case resp := <-result:
		if !resp.OK || resp.Result["saved"] != true {
			t.Fatalf("explicit save was not confirmed: %+v", resp)
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("explicit save did not finish")
	}
	if !s.autosave.current(current) {
		t.Fatal("explicit save canceled a mutation newer than its request")
	}
	requireAutosaveHealthState(t, base, target, "pending")
}

func TestAutosaveDispatchNativeVerdictVisibleInHealthAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result map[string]any
		state  string
	}{
		{"confirmed", map[string]any{"saved": true, "writtenId": "keep"}, "saved"},
		{"false", map[string]any{"saved": false, "reason": "host declined", "writtenId": "keep"}, "failed"},
		{"missing", map[string]any{"reason": "unknown"}, "failed"},
		{"string", map[string]any{"saved": "true"}, "failed"},
		{"partial", map[string]any{"saved": true, "partial": true, "reason": "incomplete"}, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, base := newAutosaveDispatchFixture(t, time.Hour)
			target := testAutosaveTarget("autosave-native", "pcb")
			requests := make(chan protocol.Request, 1)
			autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
				requests <- req
				resp := *testAutosaveResponse(target)
				resp.Result = tc.result
				return resp
			})
			s.autosave.schedule(target)
			ticket := testAutosaveTicket(t, s.autosave, target)
			requireAutosaveHealthState(t, base, target, "pending")
			s.dispatchSave(ticket)
			status := requireAutosaveHealthState(t, base, target, tc.state)
			if (tc.state == "failed") != strings.Contains(status.Error, "SAVE_UNCONFIRMED") || s.autosave.current(ticket) {
				t.Fatalf("autosave completion hidden or still pending: %+v", status)
			}
			req := <-requests
			want := autosaveRequest(ticket, req.ID)
			if req.Action != target.SaveAction || req.WindowID != target.WindowID || !reflect.DeepEqual(req.Payload, want.Payload) {
				t.Fatalf("background save lost frozen target: %+v", req)
			}
			entries := autosaveAuditEntries(t, s)
			if len(entries) != 1 || entries[0].OK != (tc.state == "saved") || !reflect.DeepEqual(entries[0].Result, tc.result) || !reflect.DeepEqual(entries[0].Payload, want.Payload) {
				t.Fatalf("background save audit is incomplete: %+v", entries)
			}
			if tc.state == "failed" && entries[0].ErrorCode != "SAVE_UNCONFIRMED" {
				t.Fatalf("failed autosave audit remained green: %+v", entries[0])
			}
		})
	}
}

func TestAutosaveDispatchFrozenTargetSurvivesForegroundContextChange(t *testing.T) {
	s, base := newAutosaveDispatchFixture(t, time.Hour)
	target := testAutosaveTarget("switched-document", "pcb")
	other := target
	other.Context.ProjectUUID, other.Context.DocumentUUID, other.Context.TabID = "new-project", "new-document", "new-tab"
	requests := make(chan protocol.Request, 1)
	c := autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
		requests <- req
		resp := *testAutosaveResponse(other)
		resp.OK = false
		resp.Result = map[string]any{"saved": false, "reason": "target context mismatch"}
		resp.Error = &protocol.ErrorInfo{Code: "TARGET_CONTEXT_MISMATCH", Message: "foreground page changed"}
		return resp
	})
	s.autosave.schedule(target)
	ticket := testAutosaveTicket(t, s.autosave, target)
	if err := wsjson.Write(s.connCtx, c, protocol.ContextMessage{
		Type: protocol.TypeContext, WindowID: other.WindowID,
		ProjectUUID: other.Context.ProjectUUID, DocumentUUID: other.Context.DocumentUUID,
		DocumentType: other.Context.DocumentType, TabID: other.Context.TabID,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for getHealth(t, base).Windows[0].Context.DocumentUUID != other.Context.DocumentUUID {
		if time.Now().After(deadline) {
			t.Fatal("mock foreground context was not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	s.dispatchSave(ticket)
	req := <-requests
	if !reflect.DeepEqual(req.Payload, autosaveRequest(ticket, req.ID).Payload) {
		t.Fatalf("background save adopted new foreground context: %+v", req)
	}
	status := requireAutosaveHealthState(t, base, target, "failed")
	if !strings.Contains(status.Error, "TARGET_CONTEXT_MISMATCH") {
		t.Fatalf("target mismatch error was lost: %+v", status)
	}
	entries := autosaveAuditEntries(t, s)
	if len(entries) != 1 || entries[0].OK || entries[0].ErrorCode != "TARGET_CONTEXT_MISMATCH" || entries[0].Result["saved"] != false {
		t.Fatalf("target mismatch audit lost native verdict: %+v", entries)
	}
}

func TestAutosaveDispatchDisconnectedTargetDoesNotTransferToPeer(t *testing.T) {
	s, base := newAutosaveDispatchFixture(t, time.Hour)
	target := testAutosaveTarget("retired-autosave-target", "pcb")
	peer := target
	peer.WindowID = "still-connected-peer"
	var calls atomic.Int32
	autosaveDispatchPeer(t, s, base, peer.WindowID, func(req protocol.Request) protocol.Response {
		calls.Add(1)
		resp := *testAutosaveResponse(peer)
		resp.Result = map[string]any{"saved": true}
		return resp
	})
	s.autosave.schedule(target)
	s.dispatchSave(testAutosaveTicket(t, s.autosave, target))
	status := requireAutosaveHealthState(t, base, target, "failed")
	if !strings.Contains(status.Error, "connector disconnected") || calls.Load() != 0 {
		t.Fatalf("disconnected target was hidden or transferred to another transport: %+v, peer calls=%d", status, calls.Load())
	}
}

func TestAutosaveDispatchOldCompletionKeepsNewMutationPending(t *testing.T) {
	for _, saved := range []bool{true, false} {
		t.Run(fmt.Sprint(saved), func(t *testing.T) {
			s, base := newAutosaveDispatchFixture(t, time.Hour)
			target := testAutosaveTarget("in-flight-autosave", "pcb")
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			defer close(release)
			autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
				entered <- struct{}{}
				select {
				case <-release:
				case <-s.connCtx.Done():
				}
				resp := *testAutosaveResponse(target)
				resp.Result = map[string]any{"saved": saved}
				return resp
			})
			s.autosave.schedule(target)
			old := testAutosaveTicket(t, s.autosave, target)
			done := make(chan struct{})
			go func() { s.dispatchSave(old); close(done) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("background save did not dispatch")
			}
			s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: target.WindowID}, Action: "pcb.component.modify"}, testAutosaveResponse(target), time.Now())
			current := testAutosaveTicket(t, s.autosave, target)
			if current.generation == old.generation {
				t.Fatal("new mutation reused in-flight generation")
			}
			release <- struct{}{}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("background save did not finish")
			}
			if !s.autosave.current(current) {
				t.Fatal("old completion discarded the new mutation")
			}
			requireAutosaveHealthState(t, base, target, "pending")
			entries := autosaveAuditEntries(t, s)
			if len(entries) != 1 || entries[0].OK != saved {
				t.Fatalf("old request verdict should remain independently auditable: %+v", entries)
			}
		})
	}
}

func TestMutationMissingIdentityWarningSurvivesHTTPAndAudit(t *testing.T) {
	for _, contextPresent := range []bool{false, true} {
		t.Run(fmt.Sprint(contextPresent), func(t *testing.T) {
			s, base := newAutosaveDispatchFixture(t, time.Hour)
			target := autosaveTarget{WindowID: "unknown-target", SaveAction: "pcb.save"}
			if contextPresent {
				target.Context.DocumentType = "pcb"
			}
			autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
				resp := protocol.Response{OK: true, Result: map[string]any{"modified": true}}
				if contextPresent {
					ctx := target.Context
					resp.Context = &ctx
					resp.MutationContext = &ctx
				}
				return resp
			})
			resp := postAction(t, base, `{"action":"pcb.component.modify","windowId":"unknown-target"}`)
			if !resp.OK || len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "autosave was not armed") {
				t.Fatalf("unavailable autosave warning lost in HTTP: %+v", resp)
			}
			status := requireAutosaveHealthState(t, base, target, "unavailable")
			if status.Error != resp.Warnings[0] {
				t.Fatalf("health and HTTP disagree: %+v", status)
			}
			entries := autosaveAuditEntries(t, s)
			if len(entries) != 1 || !entries[0].OK || !reflect.DeepEqual(entries[0].Warnings, resp.Warnings) {
				t.Fatalf("autosave warning lost in audit: %+v", entries)
			}
		})
	}
}

func TestUncertifiedMutationAndSaveEvidenceSurvivesHTTPHealthAndAudit(t *testing.T) {
	for _, action := range []string{"pcb.component.modify", "pcb.save"} {
		for _, legacy := range []bool{false, true} {
			t.Run(action+fmt.Sprint(legacy), func(t *testing.T) {
				s, base := newAutosaveDispatchFixture(t, time.Hour)
				target := testAutosaveTarget("uncertified-window", "pcb")
				var pending autosaveTicket
				if action == "pcb.save" {
					s.autosave.schedule(target)
					pending = testAutosaveTicket(t, s.autosave, target)
				}
				diagnostic := target.Context
				diagnostic.DocumentUUID = "post-action-active-document"
				autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
					resp := *testAutosaveResponse(target)
					resp.Context = &diagnostic
					resp.Result = map[string]any{"saved": true, "modified": true}
					if legacy {
						resp.MutationContext, resp.MutationContextAvailable = nil, nil
					} else {
						available := false
						resp.MutationContextAvailable = &available
						resp.MutationContextError = "handler-boundary documentUuid changed"
					}
					return resp
				})
				resp := postAction(t, base, fmt.Sprintf(`{"action":%q,"windowId":%q}`, action, target.WindowID))
				if !resp.OK || resp.Result["saved"] != true || len(resp.Warnings) != 1 || resp.Context.DocumentUUID != diagnostic.DocumentUUID {
					t.Fatalf("uncertified identity changed native response: %+v", resp)
				}
				statusTarget := target
				if legacy {
					statusTarget.Context = protocol.Context{}
				}
				status := requireAutosaveHealthState(t, base, statusTarget, "unavailable")
				if status.Error != resp.Warnings[0] {
					t.Fatalf("health hid warning: %+v", status)
				}
				if action == "pcb.save" && !s.autosave.current(pending) {
					t.Fatal("HTTP save canceled timer from unproven identity")
				}
				if action != "pcb.save" && len(s.autosave.timers) != 0 {
					t.Fatal("HTTP mutation armed from diagnostic context")
				}
				entries := autosaveAuditEntries(t, s)
				if len(entries) != 1 || !entries[0].OK || !reflect.DeepEqual(entries[0].Warnings, resp.Warnings) ||
					!reflect.DeepEqual(entries[0].Context, resp.Context) || !reflect.DeepEqual(entries[0].MutationContext, resp.MutationContext) ||
					!reflect.DeepEqual(entries[0].MutationContextAvailable, resp.MutationContextAvailable) || entries[0].MutationContextError != resp.MutationContextError {
					t.Fatalf("wire identity evidence lost in audit: %+v", entries)
				}
			})
		}
	}
}

func TestAutosaveNativeSuccessWithUncertifiedTargetRemainsUnavailable(t *testing.T) {
	for _, mode := range []string{"legacy", "drift", "certified other document"} {
		t.Run(mode, func(t *testing.T) {
			s, base := newAutosaveDispatchFixture(t, time.Hour)
			target := testAutosaveTarget("background-certification", "pcb")
			autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
				resp := *testAutosaveResponse(target)
				resp.Result = map[string]any{"saved": true}
				switch mode {
				case "legacy":
					resp.MutationContextAvailable = nil
				case "drift":
					available := false
					resp.MutationContextAvailable = &available
					resp.MutationContextError = "document changed"
				case "certified other document":
					resp.MutationContext.DocumentUUID = "other-document"
				}
				return resp
			})
			s.autosave.schedule(target)
			s.dispatchSave(testAutosaveTicket(t, s.autosave, target))
			requireAutosaveHealthState(t, base, target, "unavailable")
			entries := autosaveAuditEntries(t, s)
			if len(entries) != 1 || !entries[0].OK || entries[0].Result["saved"] != true || len(entries[0].Warnings) != 1 {
				t.Fatalf("background native success or unknown target misreported: %+v", entries)
			}
		})
	}
}

func TestAutosaveHealthOmitsInactiveState(t *testing.T) {
	for _, debounce := range []time.Duration{0, time.Hour} {
		t.Run(debounce.String(), func(t *testing.T) {
			_, base := newAutosaveDispatchFixture(t, debounce)
			resp, err := http.Get("http://" + base + "/health")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var raw map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
				t.Fatal(err)
			}
			if _, exists := raw["autosave"]; exists {
				t.Fatalf("inactive daemon exposes fictional autosave state: %+v", raw["autosave"])
			}
		})
	}
}

func TestAutosaveSystemHealthMirrorsTargetEvidence(t *testing.T) {
	s, base := newAutosaveDispatchFixture(t, time.Hour)
	for _, state := range []string{"pending", "saved", "failed"} {
		target := testAutosaveTarget("health-"+state, "pcb")
		s.autosave.schedule(target)
		if state != "pending" {
			s.autosave.finish(testAutosaveTicket(t, s.autosave, target), state, "")
		}
	}
	s.maybeAutosave(&protocol.Request{Envelope: protocol.Envelope{WindowID: "unknown-health-target"}, Action: "pcb.component.modify"}, &protocol.Response{OK: true}, time.Now())
	want := getHealth(t, base).Autosave
	resp := postAction(t, base, `{"action":"system.health"}`)
	if !resp.OK {
		t.Fatalf("daemon health action failed: %+v", resp)
	}
	data, err := json.Marshal(resp.Result["autosave"])
	if err != nil {
		t.Fatal(err)
	}
	var got []AutosaveStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(want) != 4 || !reflect.DeepEqual(got, want) {
		t.Fatalf("typed health disagrees with HTTP target evidence: got=%+v want=%+v", got, want)
	}
}

func TestExplicitSaveUnconfirmedCountsAsWriteHealthFailure(t *testing.T) {
	s, base := newAutosaveDispatchFixture(t, time.Hour)
	target := testAutosaveTarget("save-write-health", "pcb")
	autosaveDispatchPeer(t, s, base, target.WindowID, func(req protocol.Request) protocol.Response {
		resp := *testAutosaveResponse(target)
		resp.Result = map[string]any{"saved": false}
		return resp
	})
	resp := postAction(t, base, `{"action":"pcb.save","windowId":"save-write-health"}`)
	if resp.OK {
		t.Fatal("unconfirmed save remained successful")
	}
	h := getHealth(t, base)
	bucket := h.WriteHealth[target.WindowID].Actions[target.SaveAction]
	if bucket.Failures != 1 || bucket.Samples != 1 {
		t.Fatalf("unconfirmed save is falsely green in write health: %+v", bucket)
	}
}
