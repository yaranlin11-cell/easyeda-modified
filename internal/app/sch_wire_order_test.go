package app

import (
	"reflect"
	"testing"
)

func TestSchWireOrderBuildsDetachedRingFromPin(t *testing.T) {
	parts := []powerLayoutPlacement{{Pins: []powerLayoutPin{{Net: "GND", X: 10, Y: 20}}}}
	wires := []powerLayoutWire{
		{Net: "GND", Points: [][2]float64{{35, 25}, {10, 25}}},
		{Net: "GND", Points: [][2]float64{{10, 25}, {10, 20}}},
		{Net: "GND", Points: [][2]float64{{35, -25}, {35, 0}}},
		{Net: "GND", Points: [][2]float64{{35, 0}, {35, 25}}},
	}
	before := append([]powerLayoutWire(nil), wires...)
	want := []powerLayoutWire{wires[1], wires[0], wires[3], wires[2]}
	got := schPinRootedWireOrder(parts, wires)
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(wires, before) {
		t.Fatalf("wrong order or mutated source: got %#v", got)
	}
}

func TestSchWireOrderDoesNotUseDifferentNetOrDropUnrooted(t *testing.T) {
	parts := []powerLayoutPlacement{{Pins: []powerLayoutPin{{Net: "A", X: 5, Y: 0}}}}
	wires := []powerLayoutWire{
		{Net: "B", Points: [][2]float64{{0, 0}, {10, 0}}},
		{Net: "A", Points: [][2]float64{{0, 0}, {10, 0}}},
		{Net: "C", Points: [][2]float64{{30, 0}, {40, 0}}},
	}
	want := []powerLayoutWire{wires[1], wires[0], wires[2]}
	if got := schPinRootedWireOrder(parts, wires); !reflect.DeepEqual(got, want) {
		t.Fatalf("must preserve unrooted wires and match net: %#v", got)
	}
}

func TestSchWireOrderGrowsBranchFromSegmentInterior(t *testing.T) {
	parts := []powerLayoutPlacement{{Pins: []powerLayoutPin{{Net: "A", X: 0, Y: 0}}}}
	wires := []powerLayoutWire{
		{Net: "A", Points: [][2]float64{{5, 10}, {15, 10}}},
		{Net: "A", Points: [][2]float64{{5, 0}, {5, 10}}},
		{Net: "A", Points: [][2]float64{{0, 0}, {10, 0}}},
	}
	want := []powerLayoutWire{wires[2], wires[1], wires[0]}
	if got := schPinRootedWireOrder(parts, wires); !reflect.DeepEqual(got, want) {
		t.Fatalf("branch must attach to existing segment before extension: %#v", got)
	}
}
