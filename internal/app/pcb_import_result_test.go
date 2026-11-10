package app

import "testing"

func TestPcbImportResultErrorAcceptsVerifiedNoOp(t *testing.T) {
	ok := []map[string]any{
		{"imported": true, "confirm": "applied", "verified": true, "partial": false},
		{"imported": true, "confirm": "no-dialog", "noChanges": true, "verified": true, "partial": false},
	}
	for _, r := range ok {
		if err := pcbImportResultError("pcb.import_changes", r); err != nil {
			t.Errorf("%v must succeed: %v", r, err)
		}
	}
	bad := []map[string]any{
		{"imported": true, "confirm": "no-dialog", "verified": true},                     // no-op not proven by the connector
		{"imported": true, "confirm": "no-dialog", "noChanges": true, "verified": false}, // inventory check failed
		{"imported": true, "confirm": "dialog-open", "verified": false, "partial": true},
	}
	for _, r := range bad {
		if pcbImportResultError("pcb.import_changes", r) == nil {
			t.Errorf("%v must stay unverified", r)
		}
	}
}
