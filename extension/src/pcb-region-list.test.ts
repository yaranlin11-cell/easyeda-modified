/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';
import { ErrorCodes } from './protocol';

for (const [name, inventory] of [['undefined', undefined], ['null', null], ['object', {}], ['string', 'unavailable']] as const) {
	test(`region list rejects unavailable ${name} inventory rather than exposing known empty`, async t => {
		let reads = 0, writes = 0;
		(globalThis as any).eda = { pcb_PrimitiveRegion: {
			getAll: async () => { reads++; return inventory; },
			create: async () => { writes++; throw new Error('read action must not create'); },
		} };
		t.after(() => { delete (globalThis as any).eda; });
		await assert.rejects(runAction('pcb.region.list', {}), (error: any) =>
			error.code === ErrorCodes.EDA_CALL_FAILED && /region inventory is unavailable/.test(error.message));
		assert.equal(reads, 1);
		assert.equal(writes, 0);
	});
}

test('region list preserves a real empty array as known empty', async t => {
	let reads = 0;
	(globalThis as any).eda = { pcb_PrimitiveRegion: { getAll: async (layer: unknown) => {
		reads++; assert.equal(layer, undefined); return [];
	} } };
	t.after(() => { delete (globalThis as any).eda; });
	const { result }: any = await runAction('pcb.region.list', {});
	assert.deepEqual(result, { regions: [], count: 0 });
	assert.equal(reads, 1);
});

test('region list preserves populated region identity, rules and exact polygon', async t => {
	let reads = 0;
	const source = [0, 0, 'L', 100, 0, 100, 100, 0, 100, 0, 0];
	const bbox = { minX: 0, minY: 0, maxX: 100, maxY: 100 };
	const state: Record<string, unknown> = { PrimitiveId: 'synthetic-region', Layer: 12, RuleType: [5, 6, 7, 8],
		RegionName: null, ComplexPolygon: { getSource: () => source }, LineWidth: 0.2, PrimitiveLock: true };
	const region = new Proxy({}, { get: (_target, key) => String(key).startsWith('getState_')
		? () => state[String(key).slice(9)] : undefined });
	(globalThis as any).eda = {
		pcb_PrimitiveRegion: { getAll: async (layer: unknown) => { reads++; assert.equal(layer, 12); return [region]; } },
		pcb_Primitive: { getPrimitivesBBox: async (ids: unknown) => { assert.deepEqual(ids, ['synthetic-region']); return bbox; } },
	};
	t.after(() => { delete (globalThis as any).eda; });
	const { result }: any = await runAction('pcb.region.list', { layer: 12 });
	assert.equal(reads, 1);
	assert.deepEqual(result, { regions: [{ primitiveId: 'synthetic-region', layer: 12,
		ruleType: [5, 6, 7, 8], ruleTypeNames: ['no-wires', 'no-fills', 'no-pours', 'no-inner-electrical'],
		regionName: null, geometryAvailable: true, source, bbox, lineWidth: 0.2, locked: true }], count: 1 });
});
