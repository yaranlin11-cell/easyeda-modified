import type { NativePlacedDeviceSource } from './placed-device-identity';

type Row = Record<string, unknown>;
const object = (value: unknown): Row | undefined => value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Row : undefined;
export interface NativeDeviceSourceEntry { deviceUuid: string; documentSource: string }

/** DEVICE metadata from the same bounded export, with one exact current page. */
export function projectDeviceSourceInventory(text: string, documentUuid: string): NativeDeviceSourceEntry[] {
	if (!documentUuid || text.length > 64 * 1024 * 1024) throw new Error('native device source target is missing or exceeds 64 MiB');
	const rows = text.split(/\r?\n/);
	if (rows.length > 200000) throw new Error('native device source exceeds 200000 rows');
	let last = rows.length - 1;
	while (last >= 0 && !rows[last].trim()) last--;
	const entries: NativeDeviceSourceEntry[] = [];
	let current: NativeDeviceSourceEntry | undefined, pages = 0;
	for (const [index, raw] of rows.entries()) {
		let line = raw.trim();
		if (!line) continue;
		const delimiter = line.indexOf('||');
		if (delimiter < 0 || (!line.endsWith('|') && index !== last)) throw new Error('malformed native device source row');
		if (!line.endsWith('|')) { JSON.parse(line.slice(delimiter + 2)); line += '|'; }
		const header = object(JSON.parse(line.slice(0, delimiter)));
		if (!header) throw new Error('native device source header is not an object');
		if (header.type === 'DOCHEAD') {
			const payload = object(JSON.parse(line.slice(delimiter + 2, -1)));
			if (!payload) throw new Error('native device DOCHEAD is not an object');
			current = undefined;
			if (payload.docType === 'SCH_PAGE' && payload.uuid === documentUuid) pages++;
			if (payload.docType === 'DEVICE') {
				if (typeof payload.uuid !== 'string') throw new Error('DEVICE DOCHEAD has no instance UUID');
				current = { deviceUuid: payload.uuid, documentSource: line + '\n' };
				entries.push(current);
				if (entries.length > 2048) throw new Error('native project exceeds 2048 DEVICE documents');
			}
		} else if (current && header.type === 'META') {
			if (!object(JSON.parse(line.slice(delimiter + 2, -1)))) throw new Error('native DEVICE META is not an object');
			current.documentSource += line + '\n';
		}
	}
	if (pages !== 1) throw new Error(`native project contains ${pages} matching SCH_PAGE documents; one is required`);
	return entries;
}

/** Missing DEVICE origin keeps the strict search path; conflicting origin refuses. */
export function readNativeDeviceSource(inventory: NativeDeviceSourceEntry[], instanceUuid: string): { source?: NativePlacedDeviceSource; error?: string } {
	const entries = inventory.filter(entry => entry.deviceUuid === instanceUuid);
	if (!entries.length) return {};
	if (!/^[0-9a-f]{16}$/i.test(instanceUuid) || entries.length !== 1) return { error: 'native DEVICE source owner is invalid or duplicated' };
	let heads = 0, metas = 0, source: NativePlacedDeviceSource | undefined;
	try {
		for (const raw of entries[0].documentSource.split(/\r?\n/)) {
			const line = raw.trim();
			if (!line) continue;
			const delimiter = line.indexOf('||');
			if (delimiter < 0 || !line.endsWith('|')) throw new Error('malformed DEVICE metadata row');
			const header = object(JSON.parse(line.slice(0, delimiter))), payload = object(JSON.parse(line.slice(delimiter + 2, -1)));
			if (!header || !payload) throw new Error('DEVICE metadata is not a pair of objects');
			if (header.type === 'DOCHEAD') {
				if (++heads !== 1 || payload.docType !== 'DEVICE' || payload.uuid !== instanceUuid) throw new Error('DEVICE DOCHEAD does not identify exactly this owner');
			} else if (header.type === 'META') {
				if (heads !== 1 || ++metas !== 1) throw new Error('DEVICE META must uniquely follow its DOCHEAD');
				const origin = typeof payload.source === 'string' ? /^([0-9a-f]{32})\|([^|\s]+)$/i.exec(payload.source) : null;
				const attrs = object(payload.attributes);
				if (!origin || !attrs) throw new Error('DEVICE source or placed footprint association is incomplete');
				const symbolOnly = (attrs.Footprint === '' || attrs.Footprint === null) && typeof attrs.Symbol === 'string' && /^[0-9a-f]{16}$/i.test(attrs.Symbol);
				if (!symbolOnly && (typeof attrs.Footprint !== 'string' || !/^[0-9a-f]{16}$/i.test(attrs.Footprint))) throw new Error('DEVICE source or placed footprint association is incomplete');
				source = { instanceUuid, uuid: origin[1], libraryUuid: origin[2], footprintInstanceUuid: symbolOnly ? '' : attrs.Footprint as string, ...(symbolOnly ? { symbolInstanceUuid: attrs.Symbol as string } : {}), sourceKind: 'project-epro2' };
			}
		}
		if (heads !== 1 || metas !== 1 || !source) throw new Error('DEVICE DOCHEAD/META evidence is incomplete');
		return { source };
	} catch (err) { return { error: `invalid native DEVICE source: ${err instanceof Error ? err.message : String(err)}` }; }
}
