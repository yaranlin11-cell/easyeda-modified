package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// Explicit circuit edits are separate from geometry. Both hashes refer to the
// exact input file bytes; every changed pin must have one complete old/new row.
// Pointers distinguish an explicit empty net / false NC from missing evidence.
type schConnectivityEdits struct {
	SchemaVersion int                      `json:"schemaVersion"`
	BeforeSHA256  string                   `json:"beforeSha256"`
	SourceSHA256  string                   `json:"sourceSha256"`
	Pins          []schPinConnectivityEdit `json:"pins"`
}

type schPinConnectivityEdit struct {
	ComponentID string                   `json:"componentId"`
	Ref         string                   `json:"ref"`
	PinNumber   string                   `json:"pinNumber"`
	PinName     *string                  `json:"pinName"`
	Before      *schPinConnectivityState `json:"before"`
	After       *schPinConnectivityState `json:"after"`
}

type schPinConnectivityState struct {
	Net         *string `json:"net"`
	NoConnected *bool   `json:"noConnected"`
}

type schConnectivityEditKey struct{ ComponentID, PinNumber string }

func schInputSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func decodeSchConnectivityEdits(raw, before, source []byte) (*schConnectivityEdits, error) {
	var edits schConnectivityEdits
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&edits); err != nil {
		return nil, fmt.Errorf("connectivity-edits: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("connectivity-edits requires exactly one JSON document")
	}
	if edits.SourceSHA256 != schInputSHA256(source) {
		return nil, fmt.Errorf("connectivity-edits: sourceSha256 differs from exact --from bytes")
	}
	if _, err := edits.index(before); err != nil {
		return nil, err
	}
	return &edits, nil
}

func (edits *schConnectivityEdits) index(before []byte) (map[schConnectivityEditKey]schPinConnectivityEdit, error) {
	if edits.SchemaVersion != 1 || len(edits.Pins) == 0 {
		return nil, fmt.Errorf("connectivity-edits requires schemaVersion:1 and a nonempty pins delta")
	}
	if edits.BeforeSHA256 != schInputSHA256(before) {
		return nil, fmt.Errorf("connectivity-edits: beforeSha256 differs from exact --before bytes")
	}
	if decoded, err := hex.DecodeString(edits.SourceSHA256); err != nil || len(decoded) != sha256.Size {
		return nil, fmt.Errorf("connectivity-edits: sourceSha256 must be SHA256 hex")
	}
	indexed := map[schConnectivityEditKey]schPinConnectivityEdit{}
	for _, pin := range edits.Pins {
		if pin.ComponentID == "" || pin.Ref == "" || pin.PinNumber == "" || pin.PinName == nil {
			return nil, fmt.Errorf("connectivity-edits requires componentId, ref, pinNumber and explicit pinName")
		}
		for _, state := range []*schPinConnectivityState{pin.Before, pin.After} {
			if state == nil || state.Net == nil || state.NoConnected == nil {
				return nil, fmt.Errorf("connectivity-edits: %s.%s requires explicit before/after net and noConnected", pin.Ref, pin.PinNumber)
			}
			if *state.NoConnected && *state.Net != "" {
				return nil, fmt.Errorf("connectivity-edits: %s.%s cannot be both connected and NC", pin.Ref, pin.PinNumber)
			}
		}
		if *pin.Before.Net == *pin.After.Net && *pin.Before.NoConnected == *pin.After.NoConnected {
			return nil, fmt.Errorf("connectivity-edits: %s.%s is an unchanged redundant delta", pin.Ref, pin.PinNumber)
		}
		key := schConnectivityEditKey{pin.ComponentID, pin.PinNumber}
		if _, duplicate := indexed[key]; duplicate {
			return nil, fmt.Errorf("connectivity-edits: duplicate %s.%s", pin.Ref, pin.PinNumber)
		}
		indexed[key] = pin
	}
	return indexed, nil
}

func checkSchPinConnectivityEdit(edits map[schConnectivityEditKey]schPinConnectivityEdit, id, ref, number, name string, pin map[string]any, net string, nc bool) error {
	oldNet, netKnown := pin["net"].(string)
	oldNC, ncKnown := pin["noConnected"].(bool)
	if !netKnown || !ncKnown {
		return fmt.Errorf("connectivity-edits: %s.%s source net/NC unavailable", ref, number)
	}
	key := schConnectivityEditKey{id, number}
	delta, listed := edits[key]
	if oldNet == net && oldNC == nc {
		if listed {
			return fmt.Errorf("connectivity-edits: %s.%s is unchanged but listed", ref, number)
		}
		return nil
	}
	if !listed {
		return fmt.Errorf("connectivity-edits: missing delta for %s.%s", ref, number)
	}
	if delta.Ref != ref || *delta.PinName != name || *delta.Before.Net != oldNet || *delta.Before.NoConnected != oldNC || *delta.After.Net != net || *delta.After.NoConnected != nc {
		return fmt.Errorf("connectivity-edits: %s.%s identity or before/after state differs from source/target", ref, number)
	}
	delete(edits, key)
	return nil
}
