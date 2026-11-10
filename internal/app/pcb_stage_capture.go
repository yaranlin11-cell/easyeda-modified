package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func capturePcbStage(cfg *appConfig, window, stage, dir, mode, previousSHA string, allowStale bool, stdout, stderr io.Writer) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create stage dir: %w", err)
	}
	capture := newPCBCaptureEvidence()
	payload := map[string]any{"fitMode": mode, "fit": mode != "none"}
	if previousSHA != "" {
		payload["previousSha256"] = previousSHA
	}
	snap, snapErr := capture.read(cfg, "pcb.snapshot", window, payload)
	if snap == nil {
		snap = &actionResult{Result: map[string]any{}}
	}
	pngPath, sha, shaSource := "", "", "unavailable"
	var frame frameStats
	var imageErr error
	if snapErr != nil {
		imageErr = snapErr
	}
	if imageErr == nil {
		source := snapshotArtifact(snap)
		if source == "" {
			imageErr = fmt.Errorf("snapshot PNG artifact is missing")
		} else {
			pngPath = filepath.Join(dir, "snapshot.png")
			if filepath.Clean(source) != filepath.Clean(pngPath) {
				imageErr = copyFile(source, pngPath)
			}
			if imageErr == nil {
				sha, shaSource, imageErr = resolveStageSnapshotSHA(asString(snap.Result["sha256"]), pngPath)
				if imageErr == nil {
					for _, artifact := range snap.Artifacts {
						if artifact.Path == source && artifact.SHA256 != "" && !strings.EqualFold(artifact.SHA256, sha) {
							imageErr = fmt.Errorf("snapshot artifact SHA256 mismatch: %s != %s", artifact.SHA256, sha)
							break
						}
					}
				}
				if imageErr == nil {
					raw, readErr := os.ReadFile(pngPath)
					if readErr != nil {
						imageErr = readErr
					} else if len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
						imageErr = fmt.Errorf("snapshot is not a PNG image")
					} else {
						frame, imageErr = analyzeSnapshotFrame(pngPath)
					}
				}
			}
		}
	}
	sameImage := previousSHA != "" && sha != "" && strings.EqualFold(previousSHA, sha)
	// Older connectors call byte equality stale. Keep that flag only when the
	// caller explicitly asked for the compatibility duplicate-image gate.
	stale := previousSHA != "" && (sameImage || snapshotIsStale(snap))
	bundle := []struct {
		file, action, key string
		payload           map[string]any
	}{
		{"components.json", "pcb.components.list", "components", map[string]any{"includeBBox": true, "includePads": true}},
		{"tracks.json", "pcb.line.list", "lines", nil}, {"vias.json", "pcb.via.list", "vias", nil},
		{"pours.json", "pcb.pour.list", "pours", nil}, {"nets.json", "pcb.nets.list", "nets", nil},
		{"drc.json", "pcb.drc.check", "", nil},
	}
	files, partial := []string{}, []string{}
	availability, fileHashes := map[string]string{"snapshot.png": "unknown"}, map[string]string{}
	if imageErr != nil {
		partial = append(partial, imageErr.Error())
	} else {
		fileHashes["snapshot.png"] = sha
		availability["snapshot.png"] = "available"
	}
	var drcResult map[string]any
	for _, item := range bundle {
		availability[item.file] = "unknown"
		if capture.mismatch != nil {
			partial = append(partial, item.file+": not read after identity mismatch")
			continue
		}
		res, err := capture.read(cfg, item.action, window, item.payload)
		if err != nil || res == nil {
			partial = append(partial, fmt.Sprintf("%s: %v", item.action, err))
			continue
		}
		if capture.mismatch != nil {
			continue
		}
		if item.action == "pcb.drc.check" {
			drcResult = res.Result
		}
		known := true
		if item.key != "" {
			_, known = res.Result[item.key].([]any)
		}
		if res.Result["partial"] == true || res.Result["incomplete"] == true {
			partial = append(partial, item.file+": connector reports incomplete observations; inspect per-object availability")
		}
		if known {
			availability[item.file] = "available"
		} else {
			partial = append(partial, item.file+": missing inventory array")
		}
		if err := writeJSONFile(filepath.Join(dir, item.file), res.Result); err != nil {
			availability[item.file] = "unknown"
			partial = append(partial, fmt.Sprintf("write %s: %v", item.file, err))
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, item.file))
		if err != nil {
			availability[item.file] = "unknown"
			partial = append(partial, err.Error())
			continue
		}
		files = append(files, item.file)
		fileHashes[item.file] = sha256Hex(raw)
	}
	for _, missing := range capture.Missing {
		partial = append(partial, "capture provenance: "+missing)
	}
	drcPassed, drcKnown := drcPass(drcResult)
	if !drcKnown {
		availability["drc.json"] = "unknown"
		partial = append(partial, "native DRC verdict unavailable")
	}
	if capture.mismatch != nil {
		partial = append(partial, capture.mismatch.Error())
	}
	manifest := map[string]any{
		"stage": stage, "dir": dir, "sha256": sha, "sha256Source": shaSource, "snapshot": pngPath,
		"imageValid": imageErr == nil, "blank": frame.Blank, "stale": stale, "sameImageAsPrevious": sameImage,
		"frameContent": frame.NonBgFraction, "frameColors": frame.DistinctBins,
		"dataFiles": files, "fileHashes": fileHashes, "availability": availability, "partial": partial, "capture": capture,
		"drcPassed": drcPassed, "drcPassKnown": drcKnown, "captureKind": snap.Result["captureKind"],
		"fitMode": snap.Result["fitModeApplied"], "fitApi": snap.Result["fitApi"], "objectLevelExport": snap.Result["objectLevelExport"],
	}
	if imageErr != nil {
		manifest["imageError"] = imageErr.Error()
	}
	if err := writeJSONFile(filepath.Join(dir, "stage.json"), manifest); err != nil {
		return err
	}
	if err := json.NewEncoder(stdout).Encode(manifest); err != nil {
		return err
	}
	if capture.mismatch != nil {
		return capture.mismatch
	}
	if imageErr != nil {
		return fmt.Errorf("stage %q image verification failed: %w", stage, imageErr)
	}
	if frame.Blank {
		return fmt.Errorf("stage %q has a BLANK frame; record rendering unavailable and keep data separate", stage)
	}
	if stale && !allowStale {
		return fmt.Errorf("stage %q matches the explicit --previous-sha256 duplicate-image gate; identical pixels alone do not prove old capture", stage)
	}
	if stale {
		fmt.Fprintf(stderr, "warning: duplicate image accepted by --allow-stale; capture timing is recorded separately\n")
	}
	if !drcKnown || !drcPassed {
		fmt.Fprintf(stderr, "warning: native DRC is not verified clean; this bundle is evidence, not acceptance\n")
	}
	if len(partial) > 0 {
		fmt.Fprintf(stderr, "warning: capture has %d missing/partial observations; inspect stage.json\n", len(partial))
	}
	fmt.Fprintf(stderr, "stage %q saved → %s (%d data files)\n", stage, dir, len(files))
	return nil
}
