package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/zhoushoujianwork/easyeda-agent/internal/connectivity"
)

type schUnwiredConnectivityProvenance struct {
	Basis          string `json:"basis"`
	SnapshotSHA256 string `json:"snapshotSha256"`
	PhysicalPins   int    `json:"physicalPins"`
}

// This projection is exclusive to preserve-instances Compose. It does not
// declare native netlist collection successful or edit its original snapshot.
// Exhaustive zero-drawing inventories plus every observed NC:false independently
// prove open pins, even when that host cannot export an empty native netlist.
func schComposeKnownUnconnectedConnectivity(snapshot []byte, result map[string]any) (connectivity.Document, *schUnwiredConnectivityProvenance, error) {
	var empty connectivity.Document
	if !schComposeFullyUnwired(result) {
		return empty, nil, fmt.Errorf("known-unconnected source requires complete active-page zero-drawing and physical pin evidence")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return empty, nil, err
	}
	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		return empty, nil, err
	}
	// Build a separate semantic input from copied instance facts. Availability
	// flags describe the original collector, not this independently proven graph;
	// they remain unchanged in the baseline and its exact SourceScene guard.
	parts := []any{}
	pinCount := 0
	for _, item := range source["components"].([]any) {
		part := item.(map[string]any)
		if part["componentType"] != "part" {
			continue
		}
		projected := map[string]any{}
		for _, key := range []string{"componentType", "designator", "uniqueId", "otherProperty", "device", "component", "footprint", "x", "y", "rotation", "mirror", "bbox"} {
			if value, exists := part[key]; exists {
				projected[key] = value
			}
		}
		pins := part["pins"].([]any)
		for _, item := range pins {
			item.(map[string]any)["net"] = ""
			pinCount++
		}
		projected["pins"] = pins
		parts = append(parts, projected)
	}
	doc, err := connectivity.FromRead(map[string]any{"components": parts})
	if err != nil {
		return empty, nil, err
	}
	digest := sha256.Sum256(snapshot)
	evidence := &schUnwiredConnectivityProvenance{Basis: "complete-active-page-zero-drawing", SnapshotSHA256: hex.EncodeToString(digest[:]), PhysicalPins: pinCount}
	return doc, evidence, nil
}
