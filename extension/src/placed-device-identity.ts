import { readDeviceFootprint } from './util';

type RecordValue = Record<string, unknown>;
export interface NativePlacedFootprintSource {
	instanceUuid: string;
	uuid: string;
	libraryUuid: string;
	sourceKind?: 'project-epro2';
}
export interface NativePlacedDeviceSource {
	instanceUuid: string;
	uuid: string;
	libraryUuid: string;
	footprintInstanceUuid: string;
	symbolInstanceUuid?: string;
	sourceKind: 'project-epro2';
}
export interface PlacedIdentityReads {
	byLcsc: (id: string) => Promise<unknown>;
	search: (query: string, libraryUuid: string) => Promise<unknown>;
	get: (uuid: string, libraryUuid: string) => Promise<unknown>;
	assertContext: () => Promise<void>;
}
export interface NativePlacedDeviceProof {
	device?: { uuid: string; libraryUuid: string; via: string };
	lcsc?: string;
	deviceFootprint?: string;
	footprintSource?: NativePlacedFootprintSource;
	reason?: string;
	candidates?: RecordValue[];
}

const record = (value: unknown): RecordValue =>
	value !== null && typeof value === 'object' && !Array.isArray(value) ? value as RecordValue : {};
const text = (value: unknown): string => typeof value === 'string' ? value : '';
const libraryUuid = (value: unknown): boolean => /^[0-9a-f]{32}$/i.test(text(value));
const libraryScope = (value: unknown): boolean => /^[^|\s]+$/.test(text(value));
const stableName = (value: unknown): string => text(value).startsWith('=') ? '' : text(value);
const candidateValues = (hit: RecordValue, field: 'supplierId' | 'manufacturerId'): string[] =>
	[text(hit[field]), text(record(hit.otherProperty)[field === 'supplierId' ? 'Supplier Part' : 'Manufacturer Part'])].filter(Boolean);
const candidateField = (hit: RecordValue, field: 'supplierId' | 'manufacturerId'): string => candidateValues(hit, field)[0] || '';

/** Fill absent search evidence only; a nonempty conflict never becomes a guess. */
export function hydratePlacedIdentityCandidate(hit: RecordValue, value: unknown): { candidate?: RecordValue; error?: string } {
	const detail = record(value), property = record(detail.property);
	if (!libraryUuid(hit.uuid) || !libraryScope(hit.libraryUuid)
		|| detail.uuid !== hit.uuid || detail.libraryUuid !== hit.libraryUuid) {
		return { error: 'device.get identity is missing or conflicts with the search candidate' };
	}
	const searched = readDeviceFootprint(hit), actual = readDeviceFootprint({ association: detail.association });
	if (!libraryUuid(actual.uuid) || !libraryScope(actual.libraryUuid)) return { error: 'device.get has no complete library footprint association' };
	for (const field of ['uuid', 'libraryUuid'] as const) {
		if (searched[field] && searched[field] !== actual[field]) return { error: `device.get footprint ${field} conflicts with nonempty search evidence` };
	}
	for (const field of ['supplierId', 'manufacturerId'] as const) {
		const actualValue = text(property[field]);
		if (candidateValues(hit, field).some(value => value !== actualValue)) return { error: `device.get ${field} conflicts with nonempty search evidence` };
	}
	const searchedName = stableName(hit.name), actualName = stableName(detail.name) || stableName(property.name);
	if (searchedName && !actualName) return { error: 'device.get has no stable name for nonempty search evidence' };
	if (searchedName && actualName && searchedName !== actualName) return { error: 'device.get name conflicts with nonempty search evidence' };
	return { candidate: {
		...hit,
		name: searchedName || actualName,
		supplierId: candidateField(hit, 'supplierId') || text(property.supplierId),
		manufacturerId: candidateField(hit, 'manufacturerId') || text(property.manufacturerId),
		footprint: { uuid: searched.uuid || actual.uuid, libraryUuid: searched.libraryUuid || actual.libraryUuid, name: searched.name || actual.name },
	} };
}

/**
 * Recover a library device from an independently parsed native 16→32 source.
 * A same-export DEVICE origin selects one exact asset for official detail;
 * without it, full LCSC and original-library queries are merged. Names only
 * select queries and never establish a footprint identity.
 */
export async function proveNativePlacedDevice(
	snapshot: RecordValue,
	source: NativePlacedFootprintSource,
	reads: PlacedIdentityReads,
	deviceSource?: NativePlacedDeviceSource,
): Promise<NativePlacedDeviceProof> {
	const instance = readDeviceFootprint(snapshot), placed = record(snapshot.device);
	const supplier = text(snapshot.supplierId);
	const mpn = text(snapshot.manufacturerId);
	const name = stableName(record(snapshot.component).name) || stableName(snapshot.name) || stableName(placed.name);
	const query = mpn || name, originalLibrary = text(placed.libraryUuid);
	const failure = (reason: string, candidates: RecordValue[] = []): NativePlacedDeviceProof => ({ reason, candidates });
	if (instance.uuid !== source.instanceUuid || !/^[0-9a-f]{16}$/i.test(source.instanceUuid)
		|| instance.libraryUuid !== source.libraryUuid || !libraryScope(source.libraryUuid)
		|| !libraryUuid(source.uuid) || !libraryScope(originalLibrary) || !query || (supplier && !/^C\d+$/.test(supplier))) {
		return failure('incomplete native source, original-library scope or exact model/name evidence');
	}
	const call = async (read: () => Promise<unknown>): Promise<unknown> => {
		await reads.assertContext();
		const value = await read();
		await reads.assertContext();
		return value;
	};
	if (deviceSource) {
		const componentUuid = text(record(snapshot.component).uuid);
		if (deviceSource.sourceKind !== 'project-epro2' || deviceSource.instanceUuid !== placed.uuid
			|| (componentUuid && componentUuid !== deviceSource.instanceUuid)
			|| !/^[0-9a-f]{16}$/i.test(deviceSource.instanceUuid)
			|| !libraryUuid(deviceSource.uuid) || deviceSource.libraryUuid !== originalLibrary
			|| deviceSource.footprintInstanceUuid !== source.instanceUuid) {
			return failure('native DEVICE source conflicts with the placed owner, library or footprint association');
		}
		// A same-export DEVICE origin selects exactly one library asset. Search
		// copies cannot replace or poison it; its fresh official detail still must
		// prove the model, supplier and native FOOTPRINT origin independently.
		try {
			const hydrated = hydratePlacedIdentityCandidate({ uuid: deviceSource.uuid, libraryUuid: deviceSource.libraryUuid },
				await call(() => reads.get(deviceSource.uuid, deviceSource.libraryUuid)));
			if (!hydrated.candidate) return failure(`native DEVICE device.get: ${hydrated.error}`);
			const candidate = hydrated.candidate, footprint = readDeviceFootprint(candidate);
			if ((supplier ? candidate.supplierId !== supplier : Boolean(text(candidate.supplierId)))
				|| (mpn ? candidate.manufacturerId !== mpn : candidate.name !== name)
				|| footprint.uuid !== source.uuid || footprint.libraryUuid !== source.libraryUuid) {
				return failure('native DEVICE official detail conflicts with exact supplier/model/name or native FOOTPRINT source', [candidate]);
			}
			return { device: { uuid: deviceSource.uuid, libraryUuid: deviceSource.libraryUuid, via: 'native-device-source' },
				...(supplier ? { lcsc: supplier } : {}), deviceFootprint: footprint.name, footprintSource: source };
		} catch (err) { return failure(`native DEVICE exact detail failed: ${String(err)}`); }
	}
	let lcscRows: unknown, scopedRows: unknown;
	try {
		lcscRows = supplier ? await call(() => reads.byLcsc(supplier)) : [];
		scopedRows = await call(() => reads.search(query, originalLibrary));
	} catch (err) { return failure(`complete identity candidate query failed: ${String(err)}`); }
	if (!Array.isArray(lcscRows) || !Array.isArray(scopedRows) || lcscRows.length > 128 || scopedRows.length > 128) {
		return failure('complete or scoped identity inventory unavailable or exceeds 128 candidates');
	}
	// Retain duplicate evidence until it has been checked against authoritative
	// detail. A conflicting duplicate must not disappear in a first-row dedupe.
	const rows = [...lcscRows.map(record), ...scopedRows.map(record).filter(hit => !libraryScope(hit.libraryUuid) || hit.libraryUuid === originalLibrary)];
	if (new Set(rows.map(hit => JSON.stringify([hit.libraryUuid, hit.uuid]))).size > 128) return failure('merged identity inventory exceeds 128 distinct devices');
	const matches = new Map<string, RecordValue>(), detailCache = new Map<string, unknown>();
	const errors: string[] = [], candidates: RecordValue[] = [];
	const relevantKeys = new Set(rows.filter(hit => {
		const hitSuppliers = candidateValues(hit, 'supplierId'), hitMpns = candidateValues(hit, 'manufacturerId'), hitName = stableName(hit.name);
		return (!supplier || !hitSuppliers.length || hitSuppliers.includes(supplier))
			&& (mpn ? !hitMpns.length || hitMpns.includes(mpn) : !hitName || hitName === name);
	}).map(hit => JSON.stringify([hit.libraryUuid, hit.uuid])));
	for (const hit of rows) {
		const key = JSON.stringify([hit.libraryUuid, hit.uuid]);
		if (!relevantKeys.has(key)) continue;
		if (!libraryUuid(hit.uuid) || !libraryScope(hit.libraryUuid)) { errors.push('candidate lacks a library device identity'); continue; }
		let detail: unknown;
		try {
			if (!detailCache.has(key)) detailCache.set(key, await call(() => reads.get(hit.uuid as string, hit.libraryUuid as string)));
			detail = detailCache.get(key);
		} catch (err) { errors.push(`device.get ${text(hit.uuid)} failed: ${String(err)}`); continue; }
		const proof = hydratePlacedIdentityCandidate(hit, detail);
		if (!proof.candidate) { errors.push(`${text(hit.uuid)}: ${proof.error}`); continue; }
		const candidate = proof.candidate;
		if (!candidates.some(row => row.uuid === candidate.uuid && row.libraryUuid === candidate.libraryUuid)) candidates.push(candidate);
		// Missing detail is unresolved evidence; a proven different part is merely
		// irrelevant to this exact model/source and cannot shadow the original.
		if (supplier && !text(candidate.supplierId)) { errors.push('device.get lacks exact supplier evidence'); continue; }
		if (mpn && !text(candidate.manufacturerId)) { errors.push('device.get lacks exact model evidence'); continue; }
		if (!mpn && !stableName(candidate.name)) { errors.push('device.get lacks stable exact name evidence'); continue; }
		if (supplier && candidate.supplierId !== supplier) continue;
		if (!supplier && text(candidate.supplierId)) continue;
		if (mpn ? candidate.manufacturerId !== mpn : candidate.name !== name) continue;
		const footprint = readDeviceFootprint(candidate);
		if (footprint.uuid !== source.uuid || footprint.libraryUuid !== source.libraryUuid) continue;
		matches.set(key, candidate);
	}
	try { await reads.assertContext(); } catch (err) { return failure(`identity context changed: ${String(err)}`, candidates); }
	if (errors.length) return failure(errors.join('; '), candidates);
	if (matches.size !== 1) return failure(matches.size
		? `${matches.size} distinct devices match exact LCSC/model and native footprint source — ambiguous`
		: 'no device matches exact model/name and native footprint source (package-variant mismatch)', candidates);
	const hit = [...matches.values()][0];
	return {
		device: { uuid: text(hit.uuid), libraryUuid: text(hit.libraryUuid), via: supplier ? 'lcsc-footprint-source' : 'scoped-footprint-source' },
		...(supplier ? { lcsc: supplier } : {}),
		deviceFootprint: readDeviceFootprint(hit).name,
		footprintSource: source,
	};
}

/** Exact same-export origin for a deliberately unbound schematic device. */
export async function proveNativeSymbolOnlyDevice(snapshot: RecordValue, source: NativePlacedDeviceSource, reads: PlacedIdentityReads): Promise<NativePlacedDeviceProof> {
	const placed = record(snapshot.device), symbol = record(snapshot.symbol), component = record(snapshot.component);
	const fp = readDeviceFootprint(snapshot);
	const fail = (reason: string): NativePlacedDeviceProof => ({reason});
	if (source.sourceKind !== 'project-epro2' || source.footprintInstanceUuid !== '' || !source.symbolInstanceUuid
		|| source.instanceUuid !== placed.uuid || component.uuid !== placed.uuid || source.libraryUuid !== placed.libraryUuid
		|| !/^[0-9a-f]{16}$/i.test(source.instanceUuid) || !libraryUuid(source.uuid) || !libraryScope(source.libraryUuid)
		|| symbol.uuid !== source.symbolInstanceUuid || fp.uuid || fp.name || fp.libraryUuid || !text(snapshot.manufacturerId)) return fail('symbol-only native owner/model/absence evidence incomplete or conflicting');
	try {
		await reads.assertContext();
		const detail = record(await reads.get(source.uuid, source.libraryUuid));
		await reads.assertContext();
		const prop = record(detail.property), association = record(detail.association), sf = readDeviceFootprint(detail), sy = record(association.symbol);
		if (detail.uuid !== source.uuid || detail.libraryUuid !== source.libraryUuid || sf.uuid || sf.name || (sf.libraryUuid && sf.libraryUuid !== source.libraryUuid)
			|| record(association.footprint).uuid !== ''
			|| !libraryUuid(sy.uuid) || !libraryScope(sy.libraryUuid)
			|| text(prop.manufacturerId) !== text(snapshot.manufacturerId) || text(prop.manufacturer) !== text(snapshot.manufacturer)
			|| text(prop.supplierId) !== text(snapshot.supplierId)) return fail('symbol-only official detail conflicts with exact origin/model/supplier or unbound footprint');
		return {device:{uuid:source.uuid, libraryUuid:source.libraryUuid, via:'native-symbol-only-device-source'}};
	} catch (err) {return fail(`symbol-only exact detail failed: ${String(err)}`);}
}
