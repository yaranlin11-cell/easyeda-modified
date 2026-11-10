package app

import "testing"

func TestBapFacingPairAndClearPath(t *testing.T) {
	rot := func(v float64) *float64 { return &v }
	// R.2 points right at LED.+ which points left: one straight wire, both outward.
	r2 := acPin{Designator: "R3", PinNumber: "2", X: 640, Y: 330, PinRotation: rot(0)}
	ledP := acPin{Designator: "LED1", PinNumber: "1", X: 710, Y: 330, PinRotation: rot(180)}
	if !bapFacingPair(r2, ledP) || !bapFacingPair(ledP, r2) {
		t.Fatal("facing collinear pins must qualify for a direct wire")
	}
	// Demo 2026-10-13 (LED at 0°): its anode points right, away from R → not facing.
	ledAway := ledP
	ledAway.X, ledAway.PinRotation = 770, rot(0)
	if bapFacingPair(r2, ledAway) {
		t.Fatal("an anode pointing away from R must stay with autoconnect")
	}
	offset := ledP
	offset.Y = 335
	if bapFacingPair(r2, offset) {
		t.Fatal("pins off one line need a bend, not a direct wire")
	}
	if bapFacingPair(acPin{X: 0, Y: 0}, acPin{X: 10, Y: 0}) {
		t.Fatal("unmeasured pin directions must not qualify")
	}
	owners := map[string]bool{"R3": true, "LED1": true}
	clear := []layoutComp{{Designator: "R3", BBox: &layoutBBox{MinX: 600, MinY: 325, MaxX: 640, MaxY: 335}}}
	if !bapDirectPathClear(r2, ledP, clear, nil, owners) {
		t.Fatal("owners' own bodies do not block their wire")
	}
	blocked := append(clear, layoutComp{Designator: "J2", BBox: &layoutBBox{MinX: 670, MinY: 320, MaxX: 680, MaxY: 340}})
	if bapDirectPathClear(r2, ledP, blocked, nil, owners) {
		t.Fatal("a third part between the pins blocks the direct wire")
	}
	foreign := []acPin{{Designator: "J2", PinNumber: "1", X: 680, Y: 330}}
	if bapDirectPathClear(r2, ledP, clear, foreign, owners) {
		t.Fatal("a foreign pin on the segment would be shorted")
	}
}
