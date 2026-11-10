package app

// Validate typed schematic identity evidence shared by sch list and Apply.
// Historical proof parsers below support archived evidence comparisons; fresh
// reads never dispatch an implicit debug script to repair an old connector.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var schematicPlacedIdentityRE = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)
var schematicLCSCIdentityRE = regexp.MustCompile(`^C[0-9]+$`)

// Historical fixed probe retained for offline native-source fixture regressions.
// Fresh CLI reads no longer dispatch this script or use it as a recovery path.
func schematicIdentityProbeCode(ids, footprintIDs []string, project, document string, searches ...schematicIdentitySearch) string {
	b, _ := json.Marshal(ids)
	footprints, _ := json.Marshal(footprintIDs)
	if searches == nil {
		searches = []schematicIdentitySearch{}
	}
	queries, _ := json.Marshal(searches)
	scope, _ := json.Marshal(map[string]string{"projectUuid": project, "documentUuid": document})
	return schematicIdentityArchiveCode + `
const expectedContext = ` + string(scope) + `;
async function assertIdentityContext() {
 const project = await eda.dmt_Project.getCurrentProjectInfo();
 const document = await eda.dmt_SelectControl.getCurrentDocumentInfo();
 if (!project || !document || project.uuid !== expectedContext.projectUuid || document.uuid !== expectedContext.documentUuid) throw new Error("project/document changed during official identity reads");
}
await assertIdentityContext();
const ids = ` + string(b) + `;
const footprintIds = new Set(` + string(footprints) + `);
const nativeFootprints = []; let sourceError = "";
try {
 const sources = await eda.sys_FileManager.getDocumentFootprintSources();
 if (!Array.isArray(sources)) throw new Error("native footprint sources unavailable");
 for (const item of sources) {
  if (!item || !footprintIds.has(item.footprintUuid)) continue;
  if (typeof item.documentSource !== "string") { nativeFootprints.push({footprintUuid:item.footprintUuid,metadataLines:[]}); continue; }
  const metadataLines = item.documentSource.split(/\r?\n/).filter(line => /"type"\s*:\s*"(?:DOCHEAD|META)"/.test(line));
  nativeFootprints.push({footprintUuid:item.footprintUuid,sourceKind:"document-footprint-sources",metadataLines});
 }
} catch(error) { sourceError = String(error); }
const missingFootprints = new Set([...footprintIds].filter(id => !nativeFootprints.some(item => item.footprintUuid === id)));
if (missingFootprints.size) {
 try {
  await assertIdentityContext();
  const file = await eda.sys_FileManager.getProjectFile("identity-provenance", undefined, "epro2");
  await assertIdentityContext();
  const exported = await identityFootprintsFromProject(file, missingFootprints, expectedContext.documentUuid);
  nativeFootprints.push(...exported); sourceError = "";
 } catch(error) { sourceError = "official project footprint source unavailable: " + String(error); }
}
const text = x => typeof x === "string" ? x : "";
const hits = ids.length ? await eda.lib_Device.getByLcscIds(ids, undefined, true) : [];
if (!Array.isArray(hits) || hits.length > 128) throw new Error("identity lookup is missing or exceeds 128 candidates");
const searches = ` + string(queries) + `;
for (const query of searches) {
 await assertIdentityContext();
 const found = await eda.lib_Device.search(query.query, query.libraryUuid);
 if (!Array.isArray(found) || found.length > 128) throw new Error("scoped identity search is missing or exceeds 128 candidates");
 for (const hit of found) {
  if (hit && text(hit.libraryUuid) === query.libraryUuid) hits.push(hit);
 }
}
const candidates = [];
const seen = new Set();
for (const hit of hits) {
 const key = text(hit.libraryUuid) + "/" + text(hit.uuid);
 if (seen.has(key)) continue;
 seen.add(key);
 if (seen.size > 128) throw new Error("merged identity lookup exceeds 128 candidates");
 const f = hit.footprint && typeof hit.footprint === "object" ? hit.footprint : {uuid:hit.footprintUuid,libraryUuid:hit.footprintLibraryUuid,name:hit.footprintName};
 const out = {uuid:text(hit.uuid),libraryUuid:text(hit.libraryUuid),supplierId:text(hit.supplierId),manufacturerId:text(hit.manufacturerId),name:text(hit.name),footprint:{uuid:text(f.uuid),libraryUuid:text(f.libraryUuid),name:text(f.name)}};
 try {
  const d = await eda.lib_Device.get(out.uuid,out.libraryUuid);
  if (!d) throw new Error("library Device.get returned no identity");
  const a = d.association || {}, p = d.property || {}, af = a.footprint || {};
  out.device = {uuid:text(d.uuid),libraryUuid:text(d.libraryUuid),name:text(d.name),supplierId:text(p.supplierId),manufacturerId:text(p.manufacturerId),footprint:{uuid:text(af.uuid),libraryUuid:text(af.libraryUuid)}};
  // Search rows may omit properties or associations. Only the fresh official
  // detail supplies missing fields; nonempty search/detail conflicts survive.
  if (!out.supplierId) out.supplierId = out.device.supplierId;
  if (!out.manufacturerId) out.manufacturerId = out.device.manufacturerId;
  if (!out.footprint.uuid) out.footprint.uuid = out.device.footprint.uuid;
  if (!out.footprint.libraryUuid) out.footprint.libraryUuid = out.device.footprint.libraryUuid;
 } catch(error) { out.error = String(error); }
 candidates.push(out);
}
await assertIdentityContext();
return {schemaVersion:1,query:{lcscIds:ids,allowMultiMatch:true,searches},candidates,nativeFootprints,sourceError};`
}

type schematicIdentitySearch struct {
	Query       string `json:"query"`
	LibraryUUID string `json:"libraryUuid"`
}

// The placed instance tells us which library to search. A bare C-number lookup
// can be shadowed by personal copies, even with allowMultiMatch=true. These
// searches add candidates; they never authorize a name or footprint guess.
func schematicIdentitySearches(components []map[string]any) []schematicIdentitySearch {
	unique := map[schematicIdentitySearch]bool{}
	for _, c := range components {
		query := schematicIdentityString(c, "manufacturerId")
		if query == "" {
			query = schematicStableDeviceName(c)
		}
		library := schematicIdentityString(schematicIdentityPlacedDevice(c), "libraryUuid")
		if query != "" && library != "" {
			unique[schematicIdentitySearch{Query: query, LibraryUUID: library}] = true
		}
	}
	result := make([]schematicIdentitySearch, 0, len(unique))
	for query := range unique {
		result = append(result, query)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LibraryUUID != result[j].LibraryUUID {
			return result[i].LibraryUUID < result[j].LibraryUUID
		}
		return result[i].Query < result[j].Query
	})
	return result
}

type schematicIdentityAsset struct {
	UUID        string `json:"uuid"`
	LibraryUUID string `json:"libraryUuid"`
	Name        string `json:"name,omitempty"`
}
type schematicIdentityDevice struct {
	UUID           string                 `json:"uuid"`
	LibraryUUID    string                 `json:"libraryUuid"`
	Name           string                 `json:"name"`
	SupplierID     string                 `json:"supplierId"`
	ManufacturerID string                 `json:"manufacturerId"`
	Footprint      schematicIdentityAsset `json:"footprint"`
}
type schematicIdentityCandidate struct {
	UUID           string                   `json:"uuid"`
	LibraryUUID    string                   `json:"libraryUuid"`
	SupplierID     string                   `json:"supplierId"`
	ManufacturerID string                   `json:"manufacturerId"`
	Name           string                   `json:"name"`
	Footprint      schematicIdentityAsset   `json:"footprint"`
	Device         *schematicIdentityDevice `json:"device,omitempty"`
	Error          string                   `json:"error,omitempty"`
}
type schematicNativeFootprint struct {
	FootprintUUID string   `json:"footprintUuid"`
	MetadataLines []string `json:"metadataLines"`
	SourceKind    string   `json:"sourceKind,omitempty"`
}

type schematicIdentityProbe struct {
	SchemaVersion int `json:"schemaVersion"`
	Query         struct {
		LCSCIDs         []string                  `json:"lcscIds"`
		AllowMultiMatch bool                      `json:"allowMultiMatch"`
		Searches        []schematicIdentitySearch `json:"searches"`
	} `json:"query"`
	Candidates       []schematicIdentityCandidate `json:"candidates"`
	NativeFootprints []schematicNativeFootprint   `json:"nativeFootprints"`
	SourceError      string                       `json:"sourceError,omitempty"`
}

func schematicIdentityString(m map[string]any, key string) string { v, _ := m[key].(string); return v }
func schematicIdentityMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

// Connector recovery leaves the exact original placed handle beside its
// resolved device. Verify those records through the same independent probe as
// unresolved instances, so request-local lookup shape cannot choose the guard
// evidence schema. Native 32-hex library references without a placed handle
// remain authoritative and do not take this compatibility path.
func schematicIdentityPlacedDevice(c map[string]any) map[string]any {
	d := schematicIdentityMap(c, "device")
	if isDeviceLibraryUUID(schematicIdentityString(d, "uuid")) {
		return schematicIdentityMap(c, "placedDevice")
	}
	return d
}

func needsSchematicIdentityCompat(c map[string]any) bool {
	if schematicIdentityString(c, "componentType") != "part" {
		return false
	}
	device, part, fp := schematicIdentityPlacedDevice(c), schematicIdentityMap(c, "component"), schematicIdentityMap(c, "footprint")
	supplier := schematicIdentityString(c, "supplierId")
	model := schematicIdentityString(c, "manufacturerId")
	// Personal library assets need not have a supplier C-number. They still
	// need an exact searchable model/name and a fixed placed device library.
	identityKey := schematicLCSCIdentityRE.MatchString(supplier) ||
		(supplier == "" && (model != "" || schematicStableDeviceName(c) != "") &&
			strings.TrimSpace(schematicIdentityString(device, "libraryUuid")) != "")
	return schematicPlacedIdentityRE.MatchString(schematicIdentityString(device, "uuid")) &&
		schematicPlacedIdentityRE.MatchString(schematicIdentityString(part, "uuid")) &&
		schematicIdentityString(device, "uuid") == schematicIdentityString(part, "uuid") &&
		schematicPlacedIdentityRE.MatchString(schematicIdentityString(fp, "uuid")) &&
		strings.TrimSpace(schematicIdentityString(fp, "libraryUuid")) != "" && identityKey
}

func schematicStableDeviceName(c map[string]any) string {
	for _, name := range []string{schematicIdentityString(c, "name"), schematicIdentityString(schematicIdentityMap(c, "component"), "name")} {
		if name != "" && !strings.HasPrefix(name, "={") {
			return name
		}
	}
	return ""
}

// Parse only the authoritative owner header and metadata emitted by the native
// footprint source API. Names never substitute for a missing/conflicting source.
func schematicNativeFootprintIdentity(instance string, entries []schematicNativeFootprint) (schematicIdentityAsset, error) {
	var zero schematicIdentityAsset
	var target *schematicNativeFootprint
	for i := range entries {
		if entries[i].FootprintUUID == instance {
			if target != nil {
				return zero, fmt.Errorf("duplicate native footprint source for %s", instance)
			}
			target = &entries[i]
		}
	}
	if target == nil {
		return zero, fmt.Errorf("native footprint source unavailable for %s", instance)
	}
	headers, metadata := 0, 0
	var source schematicIdentityAsset
	for _, line := range target.MetadataLines {
		left, right, ok := strings.Cut(strings.TrimSpace(line), "||")
		if !ok {
			return zero, fmt.Errorf("malformed native footprint metadata record")
		}
		right = strings.TrimSuffix(right, "|")
		var header struct {
			Type string `json:"type"`
		}
		var data map[string]any
		if json.Unmarshal([]byte(left), &header) != nil || json.Unmarshal([]byte(right), &data) != nil {
			return zero, fmt.Errorf("invalid JSON in native footprint metadata")
		}
		switch header.Type {
		case "DOCHEAD":
			headers++
			if headers != 1 || metadata != 0 || schematicIdentityString(data, "docType") != "FOOTPRINT" || schematicIdentityString(data, "uuid") != instance {
				return zero, fmt.Errorf("native footprint owner header disagrees with instance %s", instance)
			}
		case "META":
			metadata++
			if headers != 1 || metadata != 1 {
				return zero, fmt.Errorf("native footprint needs one ordered META source")
			}
			parts := strings.Split(schematicIdentityString(data, "source"), "|")
			if len(parts) != 2 || !isDeviceLibraryUUID(parts[0]) || parts[1] == "" || strings.ContainsAny(parts[1], " \t\r\n") {
				return zero, fmt.Errorf("native footprint has no exact source asset/library")
			}
			source = schematicIdentityAsset{UUID: parts[0], LibraryUUID: parts[1], Name: schematicIdentityString(data, "title")}
		default:
			return zero, fmt.Errorf("unexpected native footprint metadata record %q", header.Type)
		}
	}
	if headers != 1 || metadata != 1 {
		return zero, fmt.Errorf("native footprint source needs complete owner header and META")
	}
	return source, nil
}

func resolveSchematicIdentityCompat(c map[string]any, candidates []schematicIdentityCandidate, source schematicIdentityAsset) (schematicIdentityCandidate, error) {
	var zero schematicIdentityCandidate
	if !needsSchematicIdentityCompat(c) {
		return zero, fmt.Errorf("not a proven placed-instance identity domain")
	}
	fp := schematicIdentityMap(c, "footprint")
	if !isDeviceLibraryUUID(source.UUID) || source.LibraryUUID != schematicIdentityString(fp, "libraryUuid") {
		return zero, fmt.Errorf("native footprint source/library proof is unavailable or disagrees")
	}
	lcsc := schematicIdentityString(c, "supplierId")
	mpn := schematicIdentityString(c, "manufacturerId")
	name := schematicStableDeviceName(c)
	placedLibrary := schematicIdentityString(schematicIdentityPlacedDevice(c), "libraryUuid")
	label := lcsc
	if label == "" {
		label = mpn
		if label == "" {
			label = name
		}
	}
	var matches []schematicIdentityCandidate
	seen := map[string]bool{}
	for _, hit := range candidates {
		if hit.SupplierID != lcsc {
			continue
		}
		if lcsc == "" && hit.LibraryUUID != placedLibrary {
			continue
		}
		if hit.Error != "" || hit.Device == nil {
			return zero, fmt.Errorf("complete candidate identity unavailable for %s", label)
		}
		d := hit.Device
		if !isDeviceLibraryUUID(hit.UUID) || strings.TrimSpace(hit.LibraryUUID) == "" || d.UUID != hit.UUID || d.LibraryUUID != hit.LibraryUUID || d.SupplierID != lcsc {
			return zero, fmt.Errorf("candidate library identity or exact supplier proof disagrees for %s", label)
		}
		if mpn != "" {
			if hit.ManufacturerID != mpn || d.ManufacturerID != mpn {
				continue
			}
		} else {
			if name == "" || hit.Name != name || d.Name != name {
				continue
			}
		}
		if !isDeviceLibraryUUID(hit.Footprint.UUID) || hit.Footprint.UUID != source.UUID || d.Footprint.UUID != source.UUID || d.Footprint.LibraryUUID != source.LibraryUUID {
			continue
		}
		if hit.Footprint.LibraryUUID != "" && hit.Footprint.LibraryUUID != d.Footprint.LibraryUUID {
			continue
		}
		key := hit.LibraryUUID + "/" + hit.UUID
		if seen[key] {
			continue
		} // repeated rows do not create another asset identity
		seen[key] = true
		matches = append(matches, hit)
	}
	if len(matches) != 1 {
		return zero, fmt.Errorf("%s has %d exact supplier/model/native-footprint-source candidates; requires exactly one", label, len(matches))
	}
	resolved := schematicIdentityMap(c, "device")
	if isDeviceLibraryUUID(schematicIdentityString(resolved, "uuid")) &&
		(schematicIdentityString(resolved, "uuid") != matches[0].UUID || schematicIdentityString(resolved, "libraryUuid") != matches[0].LibraryUUID) {
		return zero, fmt.Errorf("connector resolved device conflicts with independent exact supplier/model/native-footprint-source proof")
	}
	return matches[0], nil
}

// Only a completed compatibility read may separate obsolete connector
// diagnostics from stable identity evidence. Keep the resolved device, native
// instance, source asset/library and official association in the scene.
func schematicIdentityCompatSceneProof(c map[string]any) bool {
	if _, present := c["deviceIdentityError"]; present {
		return false
	}
	if _, present := c["deviceIdentityCompatibilityError"]; present {
		return false
	}
	r := schematicIdentityMap(c, "deviceResolution")
	if r["resolver"] != "cli-legacy-placed-footprint-v1" || r["sameFootprintUUID"] != true {
		return false
	}
	supplier := schematicIdentityString(c, "supplierId")
	if supplier == "" {
		if r["via"] != "scoped-library-footprint-source" {
			return false
		}
	} else if !schematicLCSCIdentityRE.MatchString(supplier) || r["via"] != "lcsc-footprint-source" || r["lcsc"] != supplier {
		return false
	}
	d, fp := schematicIdentityMap(c, "device"), schematicIdentityMap(c, "footprint")
	instance := schematicIdentityMap(r, "instanceFootprint")
	s, association := schematicIdentityMap(r, "footprintSource"), schematicIdentityMap(r, "libraryFootprint")
	return isDeviceLibraryUUID(schematicIdentityString(d, "uuid")) && schematicIdentityString(d, "libraryUuid") != "" &&
		schematicPlacedIdentityRE.MatchString(schematicIdentityString(fp, "uuid")) &&
		schematicIdentityString(instance, "uuid") == schematicIdentityString(fp, "uuid") &&
		schematicIdentityString(instance, "libraryUuid") == schematicIdentityString(fp, "libraryUuid") &&
		schematicIdentityString(s, "instanceUuid") == schematicIdentityString(fp, "uuid") &&
		isDeviceLibraryUUID(schematicIdentityString(s, "uuid")) &&
		schematicIdentityString(s, "libraryUuid") != "" && schematicIdentityString(s, "libraryUuid") == schematicIdentityString(fp, "libraryUuid") &&
		schematicIdentityString(association, "uuid") == schematicIdentityString(s, "uuid") &&
		schematicIdentityString(association, "libraryUuid") == schematicIdentityString(s, "libraryUuid")
}

func schematicIdentityCompatStableScene(c map[string]any) {
	if !schematicIdentityCompatSceneProof(c) {
		return
	}
	delete(c, "deviceIdentityCandidates")
	r := schematicIdentityMap(c, "deviceResolution")
	asset := func(m map[string]any) map[string]any {
		return map[string]any{"uuid": schematicIdentityString(m, "uuid"), "libraryUuid": schematicIdentityString(m, "libraryUuid")}
	}
	source := asset(schematicIdentityMap(r, "footprintSource"))
	source["instanceUuid"] = schematicIdentityString(schematicIdentityMap(r, "footprintSource"), "instanceUuid")
	// Compare exact identities, not acquisition-path diagnostics or optional
	// search display names. The actual full footprint and device records remain
	// protected outside this canonical proof, as do properties and every pin.
	c["deviceResolution"] = map[string]any{
		"schemaVersion":     float64(1),
		"proofKind":         "exact-native-footprint-device-association",
		"via":               r["via"],
		"supplierId":        schematicIdentityString(c, "supplierId"),
		"device":            asset(schematicIdentityMap(c, "device")),
		"instanceFootprint": asset(schematicIdentityMap(c, "footprint")),
		"footprintSource":   source,
		"libraryFootprint":  asset(schematicIdentityMap(r, "libraryFootprint")),
	}
}

// validateSchematicTypedDeviceIdentity verifies the serialized contract of the
// typed connector resolver, not a second copy of official Device.get evidence.
// The connector has already checked the same-export DEVICE/FOOTPRINT origins,
// exact supplier/model and association with context checks around each read.
func validateSchematicTypedDeviceIdentity(c map[string]any) error {
	for _, key := range []string{"deviceIdentityError", "deviceIdentityCompatibilityError"} {
		if _, exists := c[key]; exists {
			return fmt.Errorf("existing %s must remain unresolved", key)
		}
	}
	device := schematicIdentityMap(c, "device")
	if !isDeviceLibraryUUID(schematicIdentityString(device, "uuid")) || !schematicIdentityLibraryScope(schematicIdentityString(device, "libraryUuid")) {
		return fmt.Errorf("complete 32-hex library device identity is unavailable")
	}
	_, hasPlaced := c["placedDevice"]
	_, hasResolution := c["deviceResolution"]
	if !hasPlaced && !hasResolution {
		return nil // A direct native library reference needs no instance recovery.
	}
	r := schematicIdentityMap(c, "deviceResolution")
	if r["via"] == "native-symbol-only-device-source" {
		placed, component := schematicIdentityMap(c, "placedDevice"), schematicIdentityMap(c, "component")
		source, symbol, fp := schematicIdentityMap(r, "symbolOnlySource"), schematicIdentityMap(c, "symbol"), schematicIdentityMap(c, "footprint")
		instance, library := schematicIdentityString(placed, "uuid"), schematicIdentityString(device, "libraryUuid")
		if r["footprintAbsent"] != true || fp["uuid"] != "" || schematicIdentityString(fp, "name") != "" || schematicIdentityString(fp, "libraryUuid") != "" ||
			!schematicPlacedIdentityRE.MatchString(instance) || component["uuid"] != instance || component["libraryUuid"] != library || placed["libraryUuid"] != library ||
			source["instanceUuid"] != instance || source["uuid"] != device["uuid"] || source["libraryUuid"] != library || source["sourceKind"] != "project-epro2" || source["footprintInstanceUuid"] != "" ||
			!schematicPlacedIdentityRE.MatchString(schematicIdentityString(source, "symbolInstanceUuid")) || source["symbolInstanceUuid"] != symbol["uuid"] || strings.TrimSpace(schematicIdentityString(c, "manufacturerId")) == "" {
			return fmt.Errorf("symbol-only typed origin/owner/model or explicit footprint absence is incomplete or conflicting")
		}
		if supplier, exists := c["supplierId"]; exists {
			value, ok := supplier.(string)
			if !ok || (value != "" && !schematicLCSCIdentityRE.MatchString(value)) {
				return fmt.Errorf("invalid symbol-only supplier evidence")
			}
		}
		return nil
	}
	if r["via"] != "native-device-source" || r["sameFootprintUUID"] != true {
		return fmt.Errorf("complete typed native-device-source proof is required; update the connector and read again")
	}
	placed, component := schematicIdentityMap(c, "placedDevice"), schematicIdentityMap(c, "component")
	instance := schematicIdentityString(placed, "uuid")
	library := schematicIdentityString(device, "libraryUuid")
	if !schematicPlacedIdentityRE.MatchString(instance) || schematicIdentityString(component, "uuid") != instance ||
		schematicIdentityString(placed, "libraryUuid") != library || schematicIdentityString(component, "libraryUuid") != library {
		return fmt.Errorf("native device instance owner or library conflicts with the resolved device")
	}
	fp, source := schematicIdentityMap(c, "footprint"), schematicIdentityMap(r, "footprintSource")
	fpInstance, fpLibrary := schematicIdentityString(fp, "uuid"), schematicIdentityString(fp, "libraryUuid")
	if !schematicPlacedIdentityRE.MatchString(fpInstance) || !schematicIdentityLibraryScope(fpLibrary) ||
		schematicIdentityString(source, "instanceUuid") != fpInstance || !isDeviceLibraryUUID(schematicIdentityString(source, "uuid")) ||
		schematicIdentityString(source, "libraryUuid") != fpLibrary || source["sourceKind"] != "project-epro2" {
		return fmt.Errorf("native footprint instance/source identity or library is incomplete or conflicting")
	}
	// The SDK may omit supplierId for a personal library device. Absence is
	// supplier-free; an explicitly malformed field is not valid evidence.
	supplier, supplierOK := c["supplierId"].(string)
	_, supplierPresent := c["supplierId"]
	if (supplierPresent && !supplierOK) || (supplier != "" && (!schematicLCSCIdentityRE.MatchString(supplier) || r["lcsc"] != supplier)) {
		return fmt.Errorf("supplier and native source C-number evidence disagree")
	}
	if supplier == "" {
		if value, present := r["lcsc"]; present && value != "" {
			return fmt.Errorf("supplier-free device carries nonempty or malformed C-number evidence")
		}
	}
	model, modelOK := c["manufacturerId"].(string)
	if _, present := c["manufacturerId"]; present && !modelOK {
		return fmt.Errorf("manufacturer model evidence has an invalid type")
	}
	if strings.TrimSpace(model) == "" && schematicTypedStableDeviceName(c) == "" {
		return fmt.Errorf("native source proof has no exact model or stable device name")
	}
	return nil
}

func schematicIdentityLibraryScope(value string) bool {
	return value != "" && !strings.ContainsRune(value, '|') && strings.IndexFunc(value, unicode.IsSpace) < 0
}

func schematicTypedStableDeviceName(c map[string]any) string {
	for _, owner := range []map[string]any{schematicIdentityMap(c, "component"), c, schematicIdentityMap(c, "placedDevice")} {
		name := schematicIdentityString(owner, "name")
		if strings.TrimSpace(name) != "" && !strings.HasPrefix(name, "=") {
			return name
		}
	}
	return ""
}

func schematicTypedIdentityContext(cfg *appConfig, envelope map[string]any) error {
	context := schematicIdentityMap(envelope, "context")
	project, doc := schematicIdentityString(context, "projectUuid"), schematicIdentityString(context, "documentUuid")
	if project == "" || doc == "" || context["documentType"] != "schematic" {
		return fmt.Errorf("typed identity requires a complete schematic project/document response context")
	}
	if cfg != nil {
		if cfg.project != "" && cfg.project != project && cfg.project != schematicIdentityString(context, "projectName") {
			return fmt.Errorf("typed identity response project differs from the requested project")
		}
		// --doc also accepts names, which are resolved and checked by the live
		// document guard. The response context contains only a document UUID.
		if (schematicPlacedIdentityRE.MatchString(cfg.doc) || isDeviceLibraryUUID(cfg.doc)) && cfg.doc != doc {
			return fmt.Errorf("typed identity response document differs from the requested document")
		}
	}
	return nil
}

// This shared postAction boundary is pure local validation: it never calls a
// daemon/connector action. Healthy typed responses remain byte-for-byte intact,
// and unsupported records retain every original field and diagnostic.
func validateSchematicIdentityRead(cfg *appConfig, action string, payload any, body []byte) ([]byte, error) {
	if action != "schematic.components.list" {
		return body, nil
	}
	var requested struct {
		IncludeDeviceIdentity bool `json:"includeDeviceIdentity"`
	}
	encoded, err := json.Marshal(payload)
	if err != nil || json.Unmarshal(encoded, &requested) != nil || !requested.IncludeDeviceIdentity {
		return body, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil || envelope["ok"] != true {
		return body, nil
	}
	result := schematicIdentityMap(envelope, "result")
	rows, _ := result["components"].([]any)
	changed := false
	for _, item := range rows {
		c, ok := item.(map[string]any)
		if !ok || schematicIdentityString(c, "componentType") != "part" {
			continue
		}
		problem := validateSchematicTypedDeviceIdentity(c)
		if problem == nil {
			problem = schematicTypedIdentityContext(cfg, envelope)
		}
		if problem == nil {
			continue
		}
		// Never overwrite or clear old diagnostics, including malformed values.
		// measuredSchematicDevice also validates this contract before replay.
		if _, exists := c["deviceIdentityCompatibilityError"]; !exists {
			c["deviceIdentityCompatibilityError"] = "unsupported typed device identity: " + problem.Error()
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	return json.Marshal(envelope)
}
