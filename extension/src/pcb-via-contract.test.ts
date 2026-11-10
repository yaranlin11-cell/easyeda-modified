/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pcbViaCreate, pcbViaList } from './actions';

test('via requests cannot silently discard layer pair or manufacturing fields', async () => {
	let writes = 0;
	(globalThis as any).eda = { pcb_PrimitiveVia: { create: async () => { writes++; return {}; } } };
	try {
		for (const extra of [{ viaType: 1 }, { startLayer: 1, endLayer: 15 }, { designRuleBlindViaName: 'L1-L2' }, { fill: 'copper' }, { diamter: 24 }]) {
			await assert.rejects(() => pcbViaCreate({ x: 1, y: 2, ...extra }), /does not implement fields/);
		}
		for (const dims of [{ holeDiameter: -1 }, { holeDiameter: 24, diameter: 12 }, { x: NaN }]) {
			await assert.rejects(() => pcbViaCreate({ x: 1, y: 2, ...dims }), /finite coordinates/);
		}
		assert.equal(writes, 0);
	} finally { delete (globalThis as any).eda; }
});

test('existing default via creation still forwards supported dimensions', async () => {
	let args: unknown[] = [];
	(globalThis as any).eda = { pcb_PrimitiveVia: { create: async (...values: unknown[]) => { args = values; return { getState_PrimitiveId: () => 'via' }; } } };
	try {
		const res = await pcbViaCreate({ x: 1, y: 2, net: 'N', holeDiameter: 8, diameter: 16 });
		assert.deepEqual(args, ['N', 1, 2, 8, 16]);
		assert.equal((res.result as any).primitiveId, 'via');
	} finally { delete (globalThis as any).eda; }
});

test('via readback preserves type and rule name without inventing layer pairs', async () => {
	const base = { getState_PrimitiveId: () => 'v', getState_Net: () => 'N', getState_X: () => 1, getState_Y: () => 2,
		getState_HoleDiameter: () => 8, getState_Diameter: () => 16, getState_PrimitiveLock: () => false };
	(globalThis as any).eda = { pcb_PrimitiveVia: { getAll: async () => [base, { ...base, getState_ViaType: () => 1, getState_DesignRuleBlindViaName: () => 'pair-A' }] } };
	try {
		const rows = ((await pcbViaList({})).result as any).vias;
		assert.equal(rows[0].viaMetadataAvailable, false); assert.equal(rows[0].viaType, null);
		assert.equal(rows[1].viaMetadataAvailable, true); assert.equal(rows[1].designRuleBlindViaName, 'pair-A');
		assert.equal(rows[1].startLayer, undefined);
	} finally { delete (globalThis as any).eda; }
});
