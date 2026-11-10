package app

import "testing"

func TestConnectStubDirection(t *testing.T) {
	rot := func(v float64) *float64 { return &v }
	// Live 2026-10-13 on ceshi: R2:1 (left pin, outward 180°) without
	// --direction got the connector's per-kind "right", which the preflight
	// guard rejected as wire-through-body + pin-exit-direction.
	leftPin := &acPin{X: 480, Y: 400, Designator: "R2", PinNumber: "1", PinRotation: rot(180)}
	cases := []struct {
		name        string
		explicit    string
		explicitSet bool
		pin         *acPin
		want        string
	}{
		{"pin outward wins over kind default", "", false, leftPin, "left"},
		{"explicit direction always wins", "up", true, leftPin, "up"},
		{"raw x/y keeps connector default", "", false, nil, ""},
		{"unmeasured pin keeps connector default", "", false, &acPin{X: 1, Y: 1}, ""},
		{"bbox fallback when rotation missing", "", false, &acPin{X: 20, Y: 0, OwnerBBox: &layoutBBox{MinX: -20, MinY: -5, MaxX: 20, MaxY: 5}}, "right"},
	}
	for _, c := range cases {
		if got := connectStubDirection(c.explicit, c.explicitSet, c.pin); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
