package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/zhoushoujianwork/easyeda-agent/internal/protocol"
)

func TestDispatchBindsResponseWindowToRoutedConnection(t *testing.T) {
	for _, tc := range []struct {
		name, hint, responseWindow string
		ok                         bool
	}{
		{"legacy implicit", "", "", true}, {"legacy explicit", "bound-window", "", true},
		{"matching connector", "bound-window", "bound-window", true},
		{"connector error", "", "", false}, {"connector matching error", "bound-window", "bound-window", false},
		{"conflicting success", "bound-window", "unrelated-window", true},
		{"conflicting error", "bound-window", "unrelated-window", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, cleanup := startDaemon(t)
			defer cleanup()
			c := dialConnector(t, base, "bound-window")
			defer c.Close(websocket.StatusNormalClosure, "")
			waitForWindow(t, base, "bound-window")
			go func() {
				for {
					_, data, err := c.Read(t.Context())
					if err != nil {
						return
					}
					var req protocol.Request
					if json.Unmarshal(data, &req) != nil || req.Type != protocol.TypeRequest {
						continue
					}
					response := protocol.Response{Envelope: protocol.Envelope{ID: req.ID, Type: protocol.TypeResponse, WindowID: tc.responseWindow}, OK: tc.ok,
						Result: map[string]any{"marker": "original-result"}, Context: &protocol.Context{ProjectUUID: "project", DocumentUUID: "board", DocumentType: "pcb"}}
					if !tc.ok {
						response.Error = &protocol.ErrorInfo{Code: "SDK_FAILURE", Message: "original connector error"}
					}
					if tc.responseWindow == "unrelated-window" {
						response.Artifacts = []protocol.Artifact{{ID: "rejected", MimeType: "image/png", InlineBase64: base64.StdEncoding.EncodeToString([]byte("must not persist"))}}
					}
					_ = wsjson.Write(t.Context(), c, response)
				}
			}()
			response := postAction(t, base, fmt.Sprintf(`{"action":"pcb.components.list","windowId":%q}`, tc.hint))
			if response.WindowID != "bound-window" {
				t.Fatalf("response window=%q, wanted authenticated routed window", response.WindowID)
			}
			if response.CreatedAt.IsZero() {
				t.Fatal("response time not stamped")
			}
			if tc.responseWindow == "unrelated-window" {
				if response.OK || response.Error == nil || response.Error.Code != "RESPONSE_WINDOW_MISMATCH" {
					t.Fatalf("conflicting window accepted: %+v", response)
				}
				if response.Result != nil || len(response.Artifacts) != 0 || response.Context != nil {
					t.Fatalf("untrusted success evidence retained: %+v", response)
				}
			} else {
				if response.OK != tc.ok || response.Result["marker"] != "original-result" || response.Context == nil {
					t.Fatalf("valid response changed beyond identity: %+v", response)
				}
				if !tc.ok && (response.Error == nil || response.Error.Code != "SDK_FAILURE") {
					t.Fatalf("original failure lost: %+v", response)
				}
			}
		})
	}
}

func TestDispatchWindowBindingDoesNotEchoUnresolvedClientHint(t *testing.T) {
	base, cleanup := startDaemon(t)
	defer cleanup()
	response := postAction(t, base, `{"action":"pcb.components.list","windowId":"untrusted-client-hint"}`)
	if response.OK || response.WindowID != "" {
		t.Fatalf("unresolved hint became observed window: %+v", response)
	}
}

func TestDispatchTransportFailureRetainsResolvedWindow(t *testing.T) {
	base, cleanup := startDaemon(t)
	defer cleanup()
	c := dialConnector(t, base, "disconnected-target")
	defer c.CloseNow()
	waitForWindow(t, base, "disconnected-target")
	go func() {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		for {
			var req protocol.Request
			if err := wsjson.Read(ctx, c, &req); err != nil {
				return
			}
			if req.Type == protocol.TypeRequest {
				_ = c.CloseNow()
				return
			}
		}
	}()
	response := postAction(t, base, `{"action":"pcb.components.list","windowId":"disconnected-target"}`)
	if response.OK || response.Error == nil || response.Error.Code != "DISPATCH_FAILED" || response.WindowID != "disconnected-target" {
		t.Fatalf("transport failure lost routed identity: %+v", response)
	}
}
