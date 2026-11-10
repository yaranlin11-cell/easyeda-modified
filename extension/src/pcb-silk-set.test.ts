/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';

const requested = { x: 10, y: 20, rotation: 90, fontFamily: 'Arial', fontSize: 40, lineWidth: 6, text: 'RESET', mirror: true, reverse: true };
const getterNames: Record<string, string> = { x: 'getState_X', y: 'getState_Y', rotation: 'getState_Rotation', fontFamily: 'getState_FontFamily', fontSize: 'getState_FontSize', lineWidth: 'getState_LineWidth', mirror: 'getState_Mirror', reverse: 'getState_Reverse' };

function fixture(options: { ignore?: string; noAck?: boolean; throwWrite?: boolean; missingFresh?: boolean; wrongIdentity?: boolean; staleBBox?: boolean; badGetter?: string; badValue?: unknown; throwingGetter?: boolean } = {}) {
	const writes: Array<{ id: string; props: Record<string, unknown> }> = [];
	const reads: Record<string, number> = {};
	const states: Record<string, Record<string, unknown>> = {};
	const objects: Record<string, Record<string, () => unknown>> = {};
	for (const id of ['a', 's']) {
		states[id] = { x: 0, y: 0, rotation: 0, fontFamily: 'default', fontSize: 20, lineWidth: 3, text: 'OLD', mirror: false, reverse: false };
		const obj: Record<string, () => unknown> = { getState_PrimitiveId: () => options.wrongIdentity && writes.length ? 'other' : id };
		for (const [field, name] of Object.entries({ ...getterNames, text: id === 'a' ? 'getState_Value' : 'getState_Text' })) {
			obj[name] = () => {
				if (writes.length && options.badGetter === field) {
					if (options.throwingGetter) throw new Error('unreadable field');
					return options.badValue;
				}
				return states[id][field];
			};
		}
		objects[id] = obj;
	}
	const api = (id: string) => ({
		getAll: async () => [objects[id]],
		get: async (target: string) => {
			reads[target] = (reads[target] ?? 0) + 1;
			return options.missingFresh && writes.length ? undefined : objects[target];
		},
		modify: async (target: string, props: Record<string, unknown>) => {
			writes.push({ id: target, props: { ...props } });
			for (const [key, value] of Object.entries(props)) {
				const field = key === 'value' ? 'text' : key;
				if (field !== options.ignore) states[target][field] = value;
			}
			if (options.throwWrite) throw new Error('SDK failed after applying');
			// Deliberately echo all requested properties even when an actual field
			// was ignored: this echo must never be used as verification evidence.
			return options.noAck ? undefined : { ...props };
		},
	});
	const sdk = {
		pcb_PrimitiveAttribute: api('a'), pcb_PrimitiveString: api('s'),
		pcb_PrimitiveComponent: { getAll: async () => [{ getState_Designator: () => 'REF', getState_PrimitiveId: () => 'c' }] },
		pcb_Primitive: { getPrimitivesBBox: async ([id]: string[]) => {
			if (id === 'c') return { minX: 100, minY: 100, maxX: 200, maxY: 200 };
			const state = states[id], x = options.staleBBox ? 0 : Number(state.x), y = options.staleBBox ? 0 : Number(state.y);
			return { minX: x - 2, minY: y - 3, maxX: x + 18, maxY: y + 7 };
		} },
		sys_FontManager: { getFontsList: async () => ['Arial'], addFont: async () => true },
	};
	return { sdk, states, writes, reads, objects };
}

async function set(f: ReturnType<typeof fixture>, fields: Record<string, unknown>, ids: unknown = ['a', 's']) {
	const globals = globalThis as any, previous = globals.eda;
	globals.eda = f.sdk;
	try { return (await runAction('pcb.silk.set', { primitiveIds: ids, ...fields })).result!; }
	finally { globals.eda = previous; }
}

test('ATTR and STRING verify all requested fields from fresh getters', async () => {
	const f = fixture(), r = await set(f, requested);
	assert.equal(r.verified, true); assert.equal(r.partial, false);
	assert.deepEqual(r.requestedIds, ['a', 's']); assert.deepEqual(r.appliedIds, ['a', 's']);
	const { text, ...other } = requested;
	assert.deepEqual(f.writes[0].props, { ...other, value: text });
	assert.deepEqual(f.writes[1].props, requested);
	for (const row of r.results as any[]) assert.deepEqual(row.actual, { primitiveId: row.primitiveId, ...requested });
});

test('explicit false mirror/reverse is written and verified for both primitive types', async () => {
	const f = fixture();
	for (const state of Object.values(f.states)) { state.mirror = true; state.reverse = true; }
	const r = await set(f, { mirror: false, reverse: false });
	assert.equal(r.verified, true); assert.equal(r.partial, false);
	for (const w of f.writes) assert.deepEqual(w.props, { mirror: false, reverse: false });
	for (const row of r.results as any[]) {
		assert.equal(row.actual.mirror, false); assert.equal(row.actual.reverse, false);
		assert.equal(row.actual.primitiveId, row.primitiveId); assert.equal(f.reads[row.primitiveId], 2);
	}
});

test('malformed booleans, numeric/string fields and IDs are rejected before writes', async () => {
	for (const fields of [{ mirror: 'false' }, { mirror: 0 }, { mirror: null }, { reverse: 'true' }, { reverse: undefined }, { x: '10' }, { y: NaN }, { rotation: Infinity }, { fontSize: 0 }, { lineWidth: -1 }, { fontFamily: '' }, { text: false }]) {
		const f = fixture(); await assert.rejects(() => set(f, fields)); assert.equal(f.writes.length, 0);
	}
	for (const ids of [[], [''], ['a', 'a'], [1]]) {
		const f = fixture(); await assert.rejects(() => set(f, { mirror: false }, ids)); assert.equal(f.writes.length, 0);
	}
});

test('a modify echo cannot hide any ignored requested field', async () => {
	for (const id of ['a', 's']) for (const field of Object.keys(requested)) {
		const f = fixture({ ignore: field }), r = await set(f, { [field]: (requested as Record<string, unknown>)[field] }, [id]);
		assert.equal(r.verified, false, `${id}/${field}`); assert.equal(r.partial, true);
		assert.deepEqual(r.appliedIds, [id]); assert.match((r.results as any[])[0].error, new RegExp(`${field} readback differs`));
	}
});

test('unreadable, nonfinite and wrong-type fresh getters cannot verify coercible values', async () => {
	for (const [field, value] of [['x', undefined], ['y', null], ['rotation', NaN], ['fontSize', Infinity], ['lineWidth', '6'], ['mirror', 1], ['reverse', 'true'], ['fontFamily', false], ['text', undefined]] as const) {
		const f = fixture({ badGetter: field, badValue: value }), r = await set(f, requested, ['s']);
		assert.equal(r.verified, false, field); assert.equal(r.partial, true); assert.equal(f.writes.length, 1);
	}
	const f = fixture({ badGetter: 'rotation', throwingGetter: true }), r = await set(f, requested, ['a']);
	assert.equal(r.verified, false); assert.equal((r.results as any[])[0].actual.fontSize, 40);
});

test('SDK exceptions/no acknowledgement still fresh-read, retain attempted IDs and stop the batch', async () => {
	for (const options of [{ noAck: true }, { throwWrite: true }, { missingFresh: true }, { wrongIdentity: true }]) {
		const f = fixture(options), r = await set(f, { mirror: true, rotation: 90 });
		assert.equal(r.verified, false); assert.equal(r.partial, true);
		assert.deepEqual(r.appliedIds, ['a']); assert.equal(f.writes.length, 1); assert.equal(f.reads.a, 2);
		assert.equal((r.results as any[])[1].attempted, false);
		if (!options.missingFresh) assert.equal((r.results as any[])[0].actual.rotation, 90);
	}
});

test('a missing initial primitive stops without claiming a partial write', async () => {
	const f = fixture(), r = await set(f, { reverse: false }, ['missing', 's']);
	assert.equal(r.verified, false); assert.equal(r.partial, false); assert.deepEqual(r.appliedIds, []); assert.equal(f.writes.length, 0);
});

test('align uses current geometry separately and requires fresh bbox agreement', async () => {
	const f = fixture(), r = await set(f, { align: 'center', ref: 'REF' }, ['s']);
	assert.equal(r.verified, true); assert.deepEqual((r.results as any[])[0].requested, { x: 142, y: 148 });
	const stale = fixture({ staleBBox: true }), bad = await set(stale, { align: 'center', ref: 'REF' }, ['s']);
	assert.equal(bad.verified, false); assert.equal(bad.partial, true); assert.match((bad.results as any[])[0].error, /rendered bbox/);
	for (const fields of [{ align: 'bad' }, { align: 'center', mirror: false }, { align: 'top', fontSize: 40 }]) {
		const f = fixture(); await assert.rejects(() => set(f, fields)); assert.equal(f.writes.length, 0);
	}
});
