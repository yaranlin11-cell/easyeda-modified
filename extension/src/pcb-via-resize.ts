import { readNativeVias } from './pcb-via-native';
/// <reference types="@jlceda/pro-api-types" />
import { ActionError, ErrorCodes, type ActionResult } from './protocol';

const keys = ['primitiveId', 'net', 'x', 'y', 'holeDiameter', 'diameter', 'locked', 'viaType', 'designRuleBlindViaName'] as const;
type Snapshot = Record<typeof keys[number], string | number | boolean>;
function refuse(message: string): never {
	throw new ActionError(ErrorCodes.PRECONDITION_REFUSED, `pcb.via.resize: ${message}; no writes dispatched`);
}
function object(value: unknown, allowed: readonly string[]): Record<string, unknown> {
	if (!value || typeof value !== 'object' || Array.isArray(value)) refuse('expected an object');
	const v = value as Record<string, unknown>;
	if (Object.keys(v).some(k => !allowed.includes(k)) || allowed.some(k => !(k in v))) refuse('missing or unsupported fields');
	return v;
}
function dimensions(v: Record<string, unknown>): void {
	if (typeof v.holeDiameter !== 'number' || typeof v.diameter !== 'number'
		|| !Number.isFinite(v.holeDiameter) || !Number.isFinite(v.diameter)
		|| v.holeDiameter <= 0 || v.diameter <= v.holeDiameter) refuse('require finite 0 < holeDiameter < diameter (mil)');
}
function snapshot(v: IPCB_PrimitiveVia): Snapshot {
	return { primitiveId: v.getState_PrimitiveId(), net: v.getState_Net(), x: v.getState_X(), y: v.getState_Y(),
		holeDiameter: v.getState_HoleDiameter(), diameter: v.getState_Diameter(), locked: v.getState_PrimitiveLock(),
		viaType: v.getState_ViaType(), designRuleBlindViaName: v.getState_DesignRuleBlindViaName() ?? '' };
}
function equal(a: Snapshot, b: Snapshot, tolerance = 1e-8): boolean {
	return keys.every(k => typeof a[k] === 'number' && typeof b[k] === 'number'
		? Math.abs((a[k] as number) - (b[k] as number)) <= tolerance : a[k] === b[k]);
}

/** Bounded, optimistic-concurrency update. Never retries or claims atomic rollback. */
export async function pcbViaResize(payload: Record<string, unknown>): Promise<ActionResult> {
	object(payload, ['items']);
	if (!Array.isArray(payload.items) || payload.items.length < 1 || payload.items.length > 128) refuse('items must contain 1..128 vias');
	const items = payload.items.map(raw => {
		const item = object(raw, ['expected', 'target']);
		const expected = object(item.expected, keys);
		const target = object(item.target, ['holeDiameter', 'diameter']);
		dimensions(expected); dimensions(target);
		if (typeof expected.primitiveId !== 'string' || !expected.primitiveId || typeof expected.net !== 'string'
			|| typeof expected.designRuleBlindViaName !== 'string' || expected.locked !== false
			|| !['x', 'y', 'viaType'].every(k => typeof expected[k] === 'number' && Number.isFinite(expected[k]))) refuse('invalid identity, metadata or locked via');
		return { expected: expected as Snapshot, target: target as { holeDiameter: number; diameter: number } };
	});
	const ids = items.map(i => i.expected.primitiveId as string);
	if (new Set(ids).size !== ids.length) refuse('duplicate primitive IDs');
	const before = (await readNativeVias()).filter(v => ids.includes(v.primitiveId)) as unknown as Snapshot[];
	const sdkBefore = (await eda.pcb_PrimitiveVia.get(ids)).map(snapshot);
	if (before.length !== ids.length || new Set(before.map(v => v.primitiveId)).size !== ids.length
		|| items.some(i => !before.some(v => equal(v, i.expected)))) refuse('stale, missing or changed via; reread before retry');
	const attempted: string[] = [], applied: string[] = [];
	let failure: string | undefined;
	for (const item of items) {
		const id = item.expected.primitiveId as string;
		try {
			// Recheck each object immediately before writing, in addition to batch preflight.
			const current = await eda.pcb_PrimitiveVia.get(id);
			if (!current || !sdkBefore.some(v => v.primitiveId === id && equal(snapshot(current), v))) throw new Error('via changed after batch preflight');
			attempted.push(id);
			const changed = await eda.pcb_PrimitiveVia.modify(id, item.target);
			if (!changed) throw new Error('modify returned no primitive');
			applied.push(id);
		}
		catch (err) { failure = String(err); break; }
	}
	let after: Snapshot[] = [];
	try { after = (await readNativeVias()).filter(v => ids.includes(v.primitiveId)) as unknown as Snapshot[]; }
	catch (err) { failure = `${failure ?? ''} readback: ${String(err)}`; }
	const verified = !failure && after.length === ids.length && new Set(after.map(v => v.primitiveId)).size === ids.length
		&& items.every(i => after.some(v => equal(v, { ...i.expected, ...i.target }, 0.00005)));
	return { result: { verified, partial: !verified, requested: ids.length, attempted, applied, before, after,
		readbackSource: 'native-document', serializationToleranceMil: 0.00005, failure: failure ?? (verified ? null : 'readback mismatch'), saved: false },
		warnings: verified ? [] : ['Batch may be partially applied. Stop, reread and regenerate remaining work; do not replay.'] };
}
