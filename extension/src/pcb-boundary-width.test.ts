/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';

const points = [[0, 0], [20, 0], [20, 20], [0, 20]];
const source = [0, 0, 'L', 20, 0, 20, 20, 0, 20, 0, 0];

for (const action of ['pcb.region.create', 'pcb.pour.create', 'pcb.fill.create']) {
	for (const width of [1, 0, -1, 0.200001, NaN, Infinity, null, '0.2']) {
		test(`${action} refuses unsupported width ${width} before any host call`, async t => {
			// No host method exists: rejection must happen before geometry or writes.
			(globalThis as any).eda = {};
			t.after(() => { delete (globalThis as any).eda; });
			await assert.rejects(() => runAction(action, { points, net: 'GND', lineWidth: width }),
				(error: any) => error.code === 'PRECONDITION_REFUSED' && /0.2mil/.test(error.message));
		});
	}
	for (const width of [undefined, 0.2]) {
		test(`${action} supports ${width === undefined ? 'omitted' : '0.2mil'} width`, async t => {
			let creates = 0;
			const values: Record<string, unknown> = { PrimitiveId: 'boundary', Layer: 1,
				LineWidth: 0.2, PrimitiveLock: false, Net: 'GND', PourFillMethod: 'solid',
				PourPriority: 1, RuleType: [1, 2, 3, 4, 6, 7], FillMode: 0,
				ComplexPolygon: { getSource: () => source } };
			const primitive = new Proxy({}, { get: (_target, key) => key === 'rebuildCopperRegion'
				? async () => false : String(key).startsWith('getState_')
					? () => values[String(key).slice(9)] : undefined });
			const primitiveType = action.includes('.pour.') ? 'Pour' : action.includes('.fill.') ? 'Fill' : 'Region';
			const widthArg = { Pour: 7, Fill: 4, Region: 4 }[primitiveType];
			(globalThis as any).eda = {
				pcb_MathPolygon: { createPolygon: () => ({ getSource: () => source }) },
				[`pcb_Primitive${primitiveType}`]: {
					create: async (...args: unknown[]) => { creates++; assert.equal(args[widthArg], width); return primitive; },
					getAll: async () => [primitive],
				},
			};
			t.after(() => { delete (globalThis as any).eda; });
			const { result }: any = await runAction(action, { points, net: 'GND', ruleTypes: values.RuleType,
				...(width === undefined ? {} : { lineWidth: width }) });
			assert.equal(creates, 1);
			assert.equal(result.primitiveId, 'boundary');
			assert.equal(result.verified, true); assert.equal(result.lineWidth, 0.2);
		});
	}
}

for (const mode of ['width', 'net', 'layer', 'mode', 'locked', 'geometry', 'duplicate', 'missing', 'unavailable', 'throws']) {
	test(`fill create preserves accountable partial state for fresh ${mode}`, async t => {
		let creates = 0;
		const values: Record<string, unknown> = { PrimitiveId: 'fill', Layer: mode === 'layer' ? 2 : 1,
			Net: mode === 'net' ? 'OTHER' : 'GND', LineWidth: mode === 'width' ? 1 : 0.2,
			FillMode: mode === 'mode' ? 1 : 0, PrimitiveLock: mode === 'locked',
			ComplexPolygon: { getSource: () => mode === 'geometry' ? [0, 0, 'L', 10, 0, 10, 10, 0, 0] : source } };
		const fresh = new Proxy({}, { get: (_target, key) => () => values[String(key).slice(9)] });
		(globalThis as any).eda = {
			pcb_MathPolygon: { createPolygon: () => ({ getSource: () => source }) },
			pcb_PrimitiveFill: {
				create: async () => { creates++; return { getState_PrimitiveId: () => 'fill' }; },
				getAll: async () => {
					if (mode === 'throws') throw new Error('unavailable');
					return mode === 'unavailable' ? undefined : mode === 'missing' ? [] : mode === 'duplicate' ? [fresh, fresh] : [fresh];
				},
			},
		};
		t.after(() => { delete (globalThis as any).eda; });
		const { result }: any = await runAction('pcb.fill.create', { points, net: 'GND', lineWidth: 0.2 });
		assert.equal(creates, 1); assert.equal(result.primitiveId, 'fill');
		assert.equal(result.partial, true); assert.equal(result.verified, false);
		if (['duplicate', 'missing', 'unavailable', 'throws'].includes(mode)) assert.ok(result.error);
		else assert.deepEqual(result.differences, [{ width: 'lineWidth', mode: 'fillMode' }[mode] ?? mode]);
	});
}

for (const action of ['pcb.region.create', 'pcb.fill.create']) {
	for (const mutation of ['constructor', 'create']) {
		test(`${action} retains raw requested geometry when host changes ${mutation}`, async t => {
			let creates = 0;
			const wrong = [0, 0, 'L', 10, 0, 10, 10, 0, 10, 0, 0];
			let hostSource: Array<number | string> = [];
			const poly = { getSource: () => hostSource };
			const values: Record<string, unknown> = { PrimitiveId: 'geometry-boundary', Layer: 1, Net: 'GND',
				RuleType: [2, 5, 7], FillMode: 0, PrimitiveLock: false, LineWidth: 0.2, ComplexPolygon: poly };
			const fresh = new Proxy({}, { get: (_target, key) => () => values[String(key).slice(9)] });
			const kind = action.includes('.region.') ? 'Region' : 'Fill';
			(globalThis as any).eda = {
				pcb_MathPolygon: { createPolygon: (src: Array<number | string>) => {
					assert.deepEqual(src, source, 'host input must derive from the requested points');
					hostSource = mutation === 'constructor' ? wrong.slice() : src;
					return poly;
				} },
				[`pcb_Primitive${kind}`]: {
					create: async () => {
						creates++;
						if (mutation === 'create') hostSource.splice(0, hostSource.length, ...wrong);
						return { getState_PrimitiveId: () => 'geometry-boundary' };
					},
					getAll: async () => [fresh],
				},
			};
			t.after(() => { delete (globalThis as any).eda; });
			const { result }: any = await runAction(action, { points, net: 'GND', ruleTypes: [2, 5, 7], lineWidth: 0.2 });
			assert.equal(creates, 1); assert.equal(result.primitiveId, 'geometry-boundary');
			assert.equal(result.verified, false); assert.equal(result.partial, true);
			assert.deepEqual(result.differences, ['geometry']);
			assert.deepEqual(result.source, wrong, 'preserve the actual wrong geometry for inspection');
		});
	}
}
