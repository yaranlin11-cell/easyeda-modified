package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const documentReadyBudget = 10 * time.Second

type documentReadyObservation struct {
	Ready           bool           `json:"ready"`
	Scope           string         `json:"scope"`
	Active          *actionContext `json:"active"`
	ComponentCount  int            `json:"componentCount"`
	ComponentSHA256 string         `json:"componentSha256"`
	Samples         int            `json:"samples"`
	StableSamples   int            `json:"stableSamples"`
	RequestIDs      []string       `json:"requestIds"`
	ObservedAt      string         `json:"observedAt"`
}

func newDocReadyCmd(cfg *appConfig, window *string, stdout io.Writer) *cobra.Command {
	var jsonOut bool
	var budget time.Duration
	cmd := &cobra.Command{
		Use:   "ready [uuid]",
		Short: "Read current document identity and stable component inventory without opening or saving",
		Long: `Verify that the requested UUID (or the current document when omitted) is
already active and its complete component list is stable across consecutive
reads. Every sample checks project/document/type/tab context before and after.
This command never saves, closes, opens or switches a document. A configured
--doc cannot cause navigation here. Unknown, drifting or unreadable data fails.

ready:true reports current component-inventory readiness only. It does not
prove that an earlier timed-out handler finished, that a previous reload
succeeded, or that nets/wires/geometry persisted. Preserve earlier failures and
obtain separate full readbacks and a successful save/reload cycle for acceptance.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if budget < time.Second || budget > time.Minute {
				return fmt.Errorf("--timeout must be between 1s and 1m")
			}
			win, err := resolveTargetWindow(cfg, *window)
			if err != nil {
				return err
			}
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			if target == "" && cfg.doc != "" {
				// --doc takes a page/PCB name elsewhere; resolve it here through the
				// read-only document listing so the readiness core keeps its
				// current+inventory-only read set and never navigates.
				docs, _, _, derr := discoverDocs(cfg, win)
				if derr != nil {
					return fmt.Errorf("doc ready --doc %q: cannot list documents to resolve it: %w", cfg.doc, derr)
				}
				want, rerr := resolveDoc(docs, cfg.doc)
				if rerr != nil {
					return fmt.Errorf("doc ready --doc %q: %w", cfg.doc, rerr)
				}
				target = want.UUID
			}
			observation, err := verifyDocumentReady(cfg, win, target, budget)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(stdout, observation)
			}
			fmt.Fprintf(stdout, "✓ %s %s is readable: %d stable components (%d samples); prior reload/handler status is not verified\n", observation.Active.DocumentType, observation.Active.DocumentUUID, observation.ComponentCount, observation.StableSamples)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON readiness evidence (never a reload success)")
	cmd.Flags().DurationVar(&budget, "timeout", documentReadyBudget, "total readiness-read budget after window selection (1s to 1m)")
	return cmd
}

// verifyDocumentReady never invokes navigation or queue-blocked retries. Its
// deadline includes all reads and polling delays. It proves the currently
// observed inventory, never completion of a previous abandoned handler.
func verifyDocumentReady(cfg *appConfig, window, target string, budget time.Duration) (*documentReadyObservation, error) {
	if budget <= 0 {
		return nil, fmt.Errorf("document readiness requires a positive deadline")
	}
	deadline := time.Now().Add(budget)
	readCfg := *cfg
	readCfg.doc = "" // no implicit ensureActiveDoc/open, even for stale selectors
	requestIDs := []string{}
	read := func(action string) (*actionResult, error) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("document readiness deadline exceeded")
		}
		if remaining > 5*time.Second {
			remaining = 5 * time.Second
		}
		result, err := requestActionOnce(&readCfg, action, window, nil, remaining)
		if err != nil {
			return nil, fmt.Errorf("document readiness %s failed: %w", action, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("document readiness deadline exceeded")
		}
		// document.current deliberately bypasses the connector FIFO and is
		// always unordered. It observes identity only; ordered, stable component
		// inventories provide freshness evidence. Neither read may change windows.
		if result == nil || (action != "document.current" && result.Seq.Unordered) || (result.WindowID != "" && result.WindowID != window) {
			return nil, fmt.Errorf("document readiness response has missing, unordered or drifting window identity")
		}
		if result.ID != "" {
			requestIDs = append(requestIDs, result.ID)
		}
		return result, nil
	}
	initial, err := read("document.current")
	if err != nil {
		return nil, err
	}
	identity, err := readyCurrentIdentity(initial, target, cfg.project)
	if err != nil {
		return nil, err
	}
	if cfg.doc != "" && target == "" && cfg.doc != identity.DocumentUUID {
		return nil, fmt.Errorf("document readiness --doc selector differs from the current UUID; pass the expected UUID explicitly, no navigation was attempted")
	}
	target = identity.DocumentUUID
	previousHash, stable, samples := "", 0, 0
	for {
		inventory, err := read(settleProbeAction(identity.DocumentType))
		if err != nil {
			return nil, err
		}
		if !readyContextEqual(identity, inventory.Context) || inventory.Seq.Unordered {
			return nil, fmt.Errorf("document readiness component read has missing, unordered or drifting context")
		}
		hash, count, err := readyComponentInventory(inventory.Result)
		if err != nil {
			return nil, err
		}
		current, err := read("document.current")
		if err != nil {
			return nil, err
		}
		after, err := readyCurrentIdentity(current, target, identity.ProjectUUID)
		if err != nil {
			return nil, err
		}
		if !readyContextEqual(identity, after) {
			return nil, fmt.Errorf("document readiness context changed while reading components")
		}
		samples++
		if hash == previousHash {
			stable++
		} else {
			previousHash, stable = hash, 1
		}
		required := 2
		if count == 0 {
			required = 3
		}
		if stable >= required {
			return &documentReadyObservation{Ready: true, Scope: "component-inventory-readiness", Active: after,
				ComponentCount: count, ComponentSHA256: hash, Samples: samples, StableSamples: stable,
				RequestIDs: requestIDs, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("document readiness deadline exceeded before inventory stabilized")
		}
		if remaining > docSettleInterval {
			remaining = docSettleInterval
		}
		time.Sleep(remaining)
	}
}

func readyCurrentIdentity(result *actionResult, target, project string) (*actionContext, error) {
	if result == nil || result.Context == nil {
		return nil, fmt.Errorf("document readiness requires a complete live document context")
	}
	ctx := result.Context
	if ctx.ProjectUUID == "" || ctx.DocumentUUID == "" || ctx.TabID == "" ||
		(ctx.DocumentType != "schematic" && ctx.DocumentType != "pcb") ||
		strField(result.Result, "uuid") != ctx.DocumentUUID || strField(result.Result, "documentType") != ctx.DocumentType ||
		strField(result.Result, "tabId") != ctx.TabID {
		return nil, fmt.Errorf("document readiness current document and context are incomplete or disagree")
	}
	if parent, present := result.Result["parentProjectUuid"]; present && parent != ctx.ProjectUUID {
		return nil, fmt.Errorf("document readiness parent project disagrees with live context")
	}
	if target != "" && target != ctx.DocumentUUID {
		return nil, fmt.Errorf("document readiness expected %s, observed %s; no navigation was attempted", target, ctx.DocumentUUID)
	}
	if project != "" && project != ctx.ProjectUUID && project != ctx.ProjectName {
		return nil, fmt.Errorf("document readiness project differs from requested project")
	}
	copy := *ctx
	return &copy, nil
}

func readyContextEqual(a, b *actionContext) bool {
	return a != nil && b != nil && a.ProjectUUID == b.ProjectUUID && a.DocumentUUID == b.DocumentUUID &&
		a.DocumentType == b.DocumentType && a.TabID == b.TabID
}

// Compare the full returned component records after ordering by stable ID.
// Equal counts alone cannot distinguish loading churn or object replacement.
func readyComponentInventory(result map[string]any) (string, int, error) {
	if err := readyCaptureComplete(result); err != nil {
		return "", 0, err
	}
	rows, ok := result["components"].([]any)
	if !ok {
		return "", 0, fmt.Errorf("document readiness requires a complete components array")
	}
	if count, present := result["count"]; present {
		number, ok := count.(float64)
		if !ok || number != float64(len(rows)) {
			return "", 0, fmt.Errorf("document readiness component count disagrees with inventory")
		}
	}
	ordered := make([]map[string]any, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		c, ok := row.(map[string]any)
		if !ok {
			return "", 0, fmt.Errorf("document readiness inventory contains an invalid component")
		}
		if err := readyCaptureComplete(c); err != nil {
			return "", 0, err
		}
		id := strField(c, "primitiveId")
		if strings.TrimSpace(id) == "" || seen[id] {
			return "", 0, fmt.Errorf("document readiness inventory has missing or duplicate component IDs")
		}
		seen[id] = true
		ordered = append(ordered, c)
	}
	sort.Slice(ordered, func(i, j int) bool { return strField(ordered[i], "primitiveId") < strField(ordered[j], "primitiveId") })
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return "", 0, fmt.Errorf("document readiness inventory cannot be encoded: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), len(rows), nil
}

// Optional capture fields are absent from SCH's basic list but present in PCB
// lists. Unrequested pads/bboxes are outside readiness scope; a reported failure
// or malformed completion flag is never evidence of a complete inventory.
func readyCaptureComplete(record map[string]any) error {
	for _, field := range []string{"partial", "incomplete"} {
		if value, present := record[field]; present && value != false {
			return fmt.Errorf("document readiness inventory has incomplete or invalid %s evidence", field)
		}
	}
	if value, present := record["captureErrors"]; present {
		errors, ok := value.(map[string]any)
		if !ok || len(errors) != 0 {
			return fmt.Errorf("document readiness inventory has missing or failed capture evidence")
		}
	}
	if value, present := record["availability"]; present {
		availability, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("document readiness inventory has malformed availability evidence")
		}
		for _, value := range availability {
			if value != "available" && value != "not-requested" {
				return fmt.Errorf("document readiness inventory contains unavailable capture data")
			}
		}
	}
	return nil
}
