import assert from 'node:assert/strict';
import { test } from 'node:test';
import { applyAndVerifyPourOrder, parseNativePourOrderSource, PourOrderError, preparePourOrder, verifyPourOrderReadback, type PourOrderTarget } from './pcb-pour-order';
import { runAction } from './actions';
import { ActionError, ErrorCodes } from './protocol';

const target: PourOrderTarget = { projectUuid: '0123456789abcdef0123456789abcdef', documentUuid: '0123456789abcdef', documentType: 'pcb' };
const ids = { a: '0000000000000001', b: '0000000000000002', other: '0000000000000003', created: '0000000000000004' };
type Row = { id: string; payload: Record<string, any> };
// Minimal anonymous TPcbPour fixture. Public schema and example source:
// https://modules.lceda.cn/pro-api/0.3.38.25cc6d44/pro-api-0.3.38.25cc6d44.eext
// assets/format-types.schema-NNUKW5PX.json /$defs/TPcbPour. Coordinates in mil;
// order is native and zero is valid. This fixture is source-only, not live proof.
function row(id: string, order: number, layerId = 1): Row {
	return { id, payload: { partitionId: '', groupId: 0, netName: 'GND', layerId, width: 0.2,
		name: `POUR${id}`, order, path: [['R', 0, 0, 100, 100, 0, 0]], pourType: { pourType: 'SOLID', fineness: 8 },
		keepIsland: false, locked: false, zIndex: 10, unknownFutureProperty: { preserved: true } } };
}
function source(rows: Row[], options: { uuid?: string; docType?: string; extra?: string } = {}): string {
	return JSON.stringify({ type: 'DOCHEAD' }) + '||' + JSON.stringify({ docType: options.docType ?? 'PCB', uuid: options.uuid ?? target.documentUuid, client: '1111111111111111' }) + '|\n' +
		rows.map((r, index) => JSON.stringify({ type: 'POUR', ticket: index + 1, id: r.id }) + '||' + JSON.stringify(r.payload)).join('|\n') +
		(rows.length ? '|\n' : '') + (options.extra ?? '');
}
function code(expected: string) {
	return (error: unknown) => error instanceof ActionError && error.code === expected;
}
function clone<T>(value: T): T { return JSON.parse(JSON.stringify(value)); }
interface HostOptions {
	rows?: Row[];
	sourceText?: string;
	identity?: () => { projectUuid?: string; documentUuid?: string; type?: number };
	getter?: (() => Promise<unknown>) | null;
}
function host(options: HostOptions = {}) {
	const globals = globalThis as any;
	const previousEda = globals.eda, previousTypes = globals.EDMT_EditorDocumentType;
	let rows = clone(options.rows ?? []), sourceReads = 0;
	globals.EDMT_EditorDocumentType = { HOME: -1, BLANK: 0, SCHEMATIC_PAGE: 1, PCB: 3 };
	const identity = () => ({ projectUuid: target.projectUuid, documentUuid: target.documentUuid, type: 3, ...options.identity?.() });
	globals.eda = {
		dmt_Project: { getCurrentProjectInfo: async () => ({ uuid: identity().projectUuid }) },
		dmt_SelectControl: { getCurrentDocumentInfo: async () => ({ uuid: identity().documentUuid, documentType: identity().type }) },
		sys_FileManager: options.getter === null ? {} : { getDocumentSource: async () => {
			sourceReads++;
			return options.getter ? options.getter() : options.sourceText ?? source(rows);
		} },
		pcb_PrimitivePour: { create: async () => { throw new Error('module must not create'); }, getAll: async () => { throw new Error('caller owns fresh read'); } },
	};
	return {
		rows: () => rows, setRows: (value: Row[]) => { rows = clone(value); }, sourceReads: () => sourceReads,
		restore: () => {
			if (previousEda === undefined) delete globals.eda; else globals.eda = previousEda;
			if (previousTypes === undefined) delete globals.EDMT_EditorDocumentType; else globals.EDMT_EditorDocumentType = previousTypes;
		},
	};
}
function freshPour(complete: () => Promise<unknown> = async () => undefined) {
	let asyncMode = false, writes = 0, desired: number | undefined;
	const fresh = {
		getState_PrimitiveId: () => ids.created,
		getState_Layer: () => 1,
		// Emulate the official wrong zero getter projection; native source wins.
		getState_PourPriority: () => 999,
		toAsync: () => { asyncMode = true; return fresh; },
		isAsync: () => asyncMode,
		setState_PourPriority: (priority: number) => { assert.equal(asyncMode, true); desired = priority; return fresh; },
		done: async () => { writes++; await complete(); return fresh; },
	};
	return { fresh, writes: () => writes, desired: () => desired };
}

test('native parser preserves zero, unknown fields and deep immutable snapshots', () => {
	const parsed = parseNativePourOrderSource(source([row(ids.a, 0)]), target);
	assert.equal(parsed.pours[0].order, 0);
	assert.deepEqual(parsed.pours[0].payload.unknownFutureProperty, { preserved: true });
	assert.ok(Object.isFrozen(parsed)); assert.ok(Object.isFrozen(parsed.pours));
	assert.ok(Object.isFrozen(parsed.pours[0].payload)); assert.ok(Object.isFrozen(parsed.pours[0].payload.path));
	assert.equal(parseNativePourOrderSource(source([]), target).pours.length, 0);
});

for (const invalid of [undefined, null, '', ' ', '{}', '[]', 'garbage', '\n', source([]).replace('DOCHEAD', 'CANVAS'),
	source([], { uuid: ids.other }), source([], { docType: 'SCH_PAGE' }), source([row(ids.a, 0), row(ids.a, 1)]),
	source([row(ids.a, 0)]) + source([]), source([row(ids.a, 0)]).replace('"order":0', '"order":null'),
	source([row(ids.a, 0)]).replace('"order":0,', ''), source([row(ids.a, 0)]).replace('"order":0', '"order":-1'),
	source([row(ids.a, 0)]).replace('"order":0', '"order":0.5'), source([row(ids.a, 0)]).replace('"layerId":1', '"layerId":null'),
	source([row(ids.a, 0)]).replace('"keepIsland":false,', ''), source([row(ids.a, 0)]).replace('"ticket":1', '"ticket":null'),
	source([row(ids.a, 0)]).replace(ids.a, 'bad-id'), source([], { extra: '{"type":"POUR","ticket":1,"id":"' + ids.a + '"}|||\n' }),
	source([], { extra: '{"type":"OTHER"}||broken\n' }), source([], { extra: '\n' })]) {
	test(`native parser refuses unavailable/ambiguous source ${String(invalid).slice(0, 60)}`, () => {
		assert.throws(() => parseNativePourOrderSource(invalid, target), code(ErrorCodes.PRECONDITION_REFUSED));
	});
}

test('omitted priority performs no source or identity/API read', async () => {
	const previous = (globalThis as any).eda;
	delete (globalThis as any).eda;
	try { assert.equal(await preparePourOrder(undefined, 1), undefined); }
	finally { if (previous !== undefined) (globalThis as any).eda = previous; }
});

for (const priority of [null, '0', -1, 0.5, NaN, Infinity, 9007199254740992, 1]) {
	test(`empty layer rejects invalid/out-of-range request ${String(priority)} without a write`, async () => {
		const h = host();
		try { await assert.rejects(preparePourOrder(priority, 1), code(ErrorCodes.PRECONDITION_REFUSED)); }
		finally { h.restore(); }
	});
}
for (const rows of [[row(ids.a, 1)], [row(ids.a, 0), row(ids.b, 0)], [row(ids.a, 0), row(ids.b, 2)]]) {
	test('prepare rejects non-contiguous/duplicate native order before create', async () => {
		const h = host({ rows });
		try { await assert.rejects(preparePourOrder(0, 1), code(ErrorCodes.PRECONDITION_REFUSED)); }
		finally { h.restore(); }
	});
}
test('prepare freezes complete baseline and does not inspect other-layer order as target rank', async () => {
	const h = host({ rows: [row(ids.a, 0), row(ids.other, 8, 2)] });
	try {
		const plan = (await preparePourOrder(1, 1))!;
		assert.ok(Object.isFrozen(plan)); assert.ok(Object.isFrozen(plan.target));
		assert.equal(plan.baseline.pours.length, 2); assert.equal(h.sourceReads(), 1);
		await assert.rejects(preparePourOrder(2, 1), code(ErrorCodes.PRECONDITION_REFUSED));
	} finally { h.restore(); }
});
for (const variant of ['missing-getter', 'identity-missing', 'schematic', 'native-wrong-identity', 'null-source', 'api-missing'] as const) {
	test(`prepare refuses missing/incorrect capability or identity ${variant}`, async () => {
		const h = host({ getter: variant === 'missing-getter' ? null : variant === 'null-source' ? async () => null : undefined,
			identity: variant === 'identity-missing' ? () => ({ projectUuid: undefined }) : variant === 'schematic' ? () => ({ type: 1 }) : undefined,
			sourceText: variant === 'native-wrong-identity' ? source([], { uuid: ids.a }) : undefined });
		if (variant === 'api-missing') delete (globalThis as any).eda.pcb_PrimitivePour.getAll;
		try { await assert.rejects(preparePourOrder(0, 1), (error: unknown) => error instanceof ActionError); }
		finally { h.restore(); }
	});
}

for (const requested of [0, 1, 2]) {
	test(`explicit native insertion ${requested} awaits one done and verifies all same/cross-layer fields`, async () => {
		const baseline = [row(ids.a, 0), row(ids.b, 1), row(ids.other, 7, 2)];
		const h = host({ rows: baseline });
		try {
			const plan = (await preparePourOrder(requested, 1))!;
			h.setRows([...baseline, row(ids.created, 2)]);
			const mock = freshPour(async () => {
				assert.equal(mock.desired(), requested);
				h.setRows([...baseline.map(old => ({ ...old, payload: { ...old.payload,
					order: old.payload.layerId === 1 && old.payload.order >= requested ? old.payload.order + 1 : old.payload.order } })), row(ids.created, requested)]);
			});
			const verified = await applyAndVerifyPourOrder(plan, mock.fresh);
			assert.equal(mock.writes(), 1); assert.equal(verified.verified, true); assert.equal(verified.nativeOrder, requested);
			assert.equal(verified.sdkPriorityBefore, 999); assert.equal(verified.orderWriteAttempted, true);
			assert.equal(verified.beforeNativeOrder, 2); assert.equal(verified.afterNativeOrder, requested);
			assert.equal(verified.finalOrders.find(pour => pour.primitiveId === ids.other)!.order, 7);
			assert.ok(verified.baselineSource && verified.beforeOrderSource && verified.afterOrderSource);
		} finally { h.restore(); }
	});
}
test('empty layer order zero succeeds despite misleading SDK getter', async () => {
	const h = host();
	try {
		const plan = (await preparePourOrder(0, 1))!; h.setRows([row(ids.created, 0)]);
		const mock = freshPour();
		assert.equal((await applyAndVerifyPourOrder(plan, mock.fresh)).nativeOrder, 0);
		assert.equal(mock.writes(), 1);
	} finally { h.restore(); }
});

for (const variant of ['baseline-changed', 'missing-created', 'unexpected-pour', 'wrong-default-order', 'missing-done', 'not-async', 'identity-changed'] as const) {
	test(`post-create preflight ${variant} retains created ID and attempts no order write`, async () => {
		let drifted = false;
		const baseline = [row(ids.a, 0)];
		const h = host({ rows: baseline, identity: () => drifted ? { documentUuid: ids.other } : {} });
		try {
			const plan = (await preparePourOrder(0, 1))!;
			const rows = [...baseline, row(ids.created, 1)];
			if (variant === 'baseline-changed') rows[0].payload.unknownFutureProperty.preserved = false;
			if (variant === 'missing-created') rows.pop();
			if (variant === 'unexpected-pour') rows.push(row(ids.other, 9, 2));
			if (variant === 'wrong-default-order') rows[1].payload.order = 3;
			h.setRows(rows);
			const mock = freshPour();
			if (variant === 'missing-done') delete (mock.fresh as any).done;
			if (variant === 'not-async') mock.fresh.isAsync = () => false;
			if (variant === 'identity-changed') drifted = true;
			await assert.rejects(applyAndVerifyPourOrder(plan, mock.fresh), (error: unknown) => {
				assert.ok(error instanceof PourOrderError); assert.equal(error.evidence.primitiveId, ids.created);
				assert.equal(error.evidence.orderWriteAttempted, false); assert.equal(error.code, ErrorCodes.EDA_CALL_FAILED); return true;
			});
			assert.equal(mock.writes(), 0);
		} finally { h.restore(); }
	});
}
for (const variant of ['setter-throws', 'done-throws', 'wrong-order', 'same-layer-field-change', 'other-layer-field-change', 'other-layer-order-change', 'created-field-change', 'missing-after', 'read-unavailable', 'malformed-after'] as const) {
	test(`one-shot order failure ${variant} retains full available evidence without retry/rollback`, async () => {
		let unavailable = false, malformed = false;
		const baseline = [row(ids.a, 0), row(ids.other, 3, 2)];
		const h = host({ rows: baseline, getter: async () => unavailable ? undefined : malformed ? source(h.rows()) + '{"type":"POUR"}||broken\n' : source(h.rows()) });
		try {
			const plan = (await preparePourOrder(0, 1))!; h.setRows([...baseline, row(ids.created, 1)]);
			const mock = freshPour(async () => {
				if (variant === 'done-throws') throw new Error('host write failed');
				const after = [row(ids.a, 1), row(ids.other, 3, 2), row(ids.created, 0)];
				if (variant === 'wrong-order') after[2].payload.order = 1;
				if (variant === 'same-layer-field-change') after[0].payload.netName = 'OTHER';
				if (variant === 'other-layer-field-change') after[1].payload.width = 1;
				if (variant === 'other-layer-order-change') after[1].payload.order = 4;
				if (variant === 'created-field-change') after[2].payload.path[0][1] = 20;
				if (variant === 'missing-after') after.splice(1, 1);
				if (variant === 'read-unavailable') unavailable = true;
				if (variant === 'malformed-after') malformed = true;
				h.setRows(after);
			});
			if (variant === 'setter-throws') mock.fresh.setState_PourPriority = () => { throw new Error('local setter failed'); };
			await assert.rejects(applyAndVerifyPourOrder(plan, mock.fresh), (error: unknown) => {
				assert.ok(error instanceof PourOrderError); assert.equal(error.evidence.primitiveId, ids.created);
				assert.equal(error.evidence.orderWriteAttempted, variant !== 'setter-throws');
				assert.ok(error.evidence.baselineSource && error.evidence.beforeOrderSource);
				if (variant === 'malformed-after') assert.match(error.evidence.afterOrderSource!, /broken/);
				if (variant === 'wrong-order') assert.equal(error.evidence.afterNativeOrder, 1);
				return true;
			});
			assert.equal(mock.writes(), variant === 'setter-throws' ? 0 : 1);
		} finally { h.restore(); }
	});
}
test('done remains pending and holds verification until the single write settles', async () => {
	const h = host();
	try {
		const plan = (await preparePourOrder(0, 1))!; h.setRows([row(ids.created, 0)]);
		let release!: () => void;
		const pending = new Promise<void>(resolve => { release = resolve; });
		const mock = freshPour(() => pending);
		let settled = false;
		const verification = applyAndVerifyPourOrder(plan, mock.fresh).then(value => { settled = true; return value; });
		for (let turn = 0; turn < 100 && mock.writes() === 0; turn++) await Promise.resolve();
		assert.equal(mock.writes(), 1); assert.equal(settled, false); assert.equal(h.sourceReads(), 2);
		release(); assert.equal((await verification).verified, true); assert.equal(mock.writes(), 1);
	} finally { h.restore(); }
});

for (const changed of [false, true]) {
	test(`read-only final order verification detects post-rebuild drift=${changed} without another setter`, async () => {
		const h = host();
		try {
			const plan = (await preparePourOrder(0, 1))!; h.setRows([row(ids.created, 0)]);
			const mock = freshPour(); const proof = await applyAndVerifyPourOrder(plan, mock.fresh);
			if (changed) h.rows()[0].payload.order = 1;
			if (changed) await assert.rejects(verifyPourOrderReadback(plan, proof), (error: unknown) => {
				assert.ok(error instanceof PourOrderError); assert.ok(error.evidence.beforeOrderSource && error.evidence.afterOrderSource && error.evidence.afterRebuildSource);
				assert.equal(error.evidence.afterNativeOrder, 1); return true;
			});
			else {
				const final = await verifyPourOrderReadback(plan, proof);
				assert.equal(final.verified, true); assert.equal(final.nativeOrder, 0); assert.ok(final.afterRebuildSource);
			}
			assert.equal(mock.writes(), 1);
		} finally { h.restore(); }
	});
}

for (const variant of ['zero-vs-sdk-one', 'illegal-two', 'setter-rejects', 'wrong-native-order', 'order-changes-during-rebuild'] as const) {
	test(`runAction explicit pour-order integration ${variant}`, async () => {
		const h = host();
		let creates = 0, orderWrites = 0, rebuilds = 0, polygons = 0;
		const polygon = [0, 0, 'L', 100, 0, 100, 100, 0, 100, 0, 0];
		let asyncMode = false, desired: number | undefined;
		const fresh = {
			getState_PrimitiveId: () => ids.created, getState_Net: () => 'GND', getState_Layer: () => 1,
			getState_PourFillMethod: () => 'solid', getState_PourName: () => 'GROUND', getState_PourPriority: () => 1,
			getState_LineWidth: () => 0.2, getState_PrimitiveLock: () => false, getState_ComplexPolygon: () => ({ getSource: () => polygon }),
			toAsync: () => { asyncMode = true; return fresh; }, isAsync: () => asyncMode,
			setState_PourPriority: (priority: number) => { desired = priority; return fresh; },
			done: async () => {
				orderWrites++;
				if (variant === 'setter-rejects') throw new Error('public done rejected');
				h.rows()[0].payload.order = variant === 'wrong-native-order' ? 1 : desired;
				return fresh;
			},
			rebuildCopperRegion: async () => {
				rebuilds++;
				if (variant === 'order-changes-during-rebuild') h.rows()[0].payload.order = 1;
				return true;
			},
		};
		const globals = globalThis as any;
		globals.eda.pcb_MathPolygon = { createPolygon: () => { polygons++; return { getSource: () => polygon }; } };
		globals.eda.pcb_PrimitivePour = {
			create: async (...args: unknown[]) => {
				creates++; assert.equal(args[6], undefined, 'never pass priority to the truthiness/clamping create API');
				const created = row(ids.created, 0); created.payload.name = 'GROUND'; created.payload.path = [polygon];
				h.setRows([created]); return fresh;
			},
			getAll: async () => h.rows().length ? [fresh] : [],
		};
		try {
			const payload = { points: [[0, 0], [100, 0], [100, 100], [0, 100]], net: 'GND', name: 'GROUND', lineWidth: 0.2,
				priority: variant === 'illegal-two' ? 2 : 0 };
			if (variant === 'illegal-two') {
				await assert.rejects(runAction('pcb.pour.create', payload), code(ErrorCodes.PRECONDITION_REFUSED));
				assert.equal(creates, 0); assert.equal(polygons, 0); assert.equal(orderWrites, 0); assert.equal(rebuilds, 0);
			} else {
				const { result }: any = await runAction('pcb.pour.create', payload);
				assert.equal(creates, 1); assert.equal(orderWrites, 1); assert.equal(result.primitiveId, ids.created);
				if (variant === 'zero-vs-sdk-one') {
					assert.equal(result.verified, true); assert.equal(result.priority, 0); assert.equal(result.sdkPriority, 1);
					assert.equal(result.priorityVerified, true); assert.equal(result.prioritySource, 'native-source');
					assert.equal(rebuilds, 1); assert.ok(result.orderProof.afterRebuildSource);
				} else {
					assert.equal(result.partial, true); assert.equal(result.verified, false);
					assert.equal(rebuilds, variant === 'order-changes-during-rebuild' ? 1 : 0);
					assert.equal(result.orderProof.primitiveId, ids.created); assert.equal(result.orderProof.orderWriteAttempted, true);
				}
			}
		} finally { h.restore(); }
	});
}
