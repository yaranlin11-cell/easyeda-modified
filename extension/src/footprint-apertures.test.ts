/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';

function mockHost(options: { read?: (value: any, reads: number) => any; save?: unknown; wrongId?: boolean; quantizedGet?: boolean; missingToAsync?: boolean; missingReset?: boolean; missingResetAtRead?: number; resetError?: boolean; resetErrorAtRead?: number; resetWrongId?: boolean; resetWrongIdAtRead?: number } = {}) {
	const calls: unknown[][] = [];
	let reads = 0, saves = 0, deletes = 0;
	const values = new Map<string, unknown>();
	(globalThis as any).eda = {
		dmt_SelectControl: { getCurrentDocumentInfo: async () => ({ uuid: 'fp', parentLibraryUuid: 'lib', documentType: 4, tabId: 'tab' }) },
		dmt_EditorControl: { getSplitScreenIdByTabId: async () => 'split', openLibraryDocument: async () => 'tab', activateDocument: async () => true },
		pcb_PrimitivePad: {
			getAllPrimitiveId: async () => [],
			create: async (...args: unknown[]) => { calls.push(args); const id = `pad-${args[1]}`; values.set(id, args[14]); return { getState_PrimitiveId: () => id }; },
			get: async (ids: string[]) => { reads++; return ids.map(id => {
				let refreshed = false, asyncMode = false;
				const readNumber = reads;
				return {
					getState_PrimitiveId: () => options.wrongId || (refreshed && (options.resetWrongId || options.resetWrongIdAtRead === readNumber)) ? 'wrong' : id,
					toAsync: options.missingToAsync ? undefined : function (this: unknown) { asyncMode = true; return this; },
					reset: options.missingReset || options.missingResetAtRead === readNumber ? undefined : async function (this: unknown) {
						assert.equal(asyncMode, true);
						await Promise.resolve(); // Prove the caller awaits reset before reading precise state.
						if (options.resetError || options.resetErrorAtRead === readNumber) throw new Error('reset failed');
						refreshed = true;
						return this;
					},
					getState_SolderMaskAndPasteMaskExpansion: () => {
						const value = options.read ? options.read(values.get(id), readNumber) : values.get(id);
						return options.quantizedGet && !refreshed ? Object.fromEntries(Object.entries(value as object).map(([key, v]) => [key, Math.round(Number(v) * 10) / 10])) : value;
					},
				};
			}); },
			delete: async () => { deletes++; return true; },
		},
		pcb_PrimitivePolyline: { getAllPrimitiveId: async () => [], delete: async () => true },
		pcb_Document: { save: async () => { saves++; return Object.hasOwn(options, 'save') ? options.save : true; } },
	};
	return { calls, stats: () => ({ reads, saves, deletes }) };
}

function build(apertures: unknown, count = 1) {
	return runAction('library.footprint.build', { uuid: 'fp', libraryUuid: 'lib', pads: Array.from({ length: count }, (_, i) => ({ number: String(i + 1), layer: 1, x: i * 50, y: 0, shape: ['RECT', 20, 30, 0], solderMaskAndPasteMaskExpansion: apertures })) });
}

test('footprint apertures preserve zero and negative values in the official SDK slot and fresh post-save readback', async () => {
	const host = mockHost();
	try {
		const value = { topSolderMask: 2, topPasteMask: 0, bottomSolderMask: -0.5 };
		const res: any = await build(value);
		assert.equal(res.result.verified, true);
		assert.equal(res.result.apertureVerifiedPads, 1);
		assert.equal(host.calls[0][13], undefined);
		assert.deepEqual(host.calls[0][14], value);
		assert.deepEqual(host.stats(), { reads: 2, saves: 1, deletes: 0 });
	} finally { delete (globalThis as any).eda; }
});

test('footprint refuses unobservable rule-based aperture policy before mutation', async () => {
	const host = mockHost();
	try { await assert.rejects(() => build(null), /cannot be independently verified/); assert.equal(host.calls.length, 0); }
	finally { delete (globalThis as any).eda; }
});

test('footprint refuses empty aperture objects before mutation while allowing omitted apertures', async () => {
	let host = mockHost();
	try {
		await assert.rejects(() => build({}), (error: any) => error.code === 'PRECONDITION_REFUSED' && /at least one explicit numeric/.test(error.message));
		assert.equal(host.calls.length, 0);
		assert.deepEqual(host.stats(), { reads: 0, saves: 0, deletes: 0 });
	} finally { delete (globalThis as any).eda; }
	host = mockHost();
	try {
		const res: any = await build(undefined);
		assert.equal(res.result.verified, true);
		assert.equal(res.result.apertureVerifiedPads, 0);
		assert.deepEqual(host.stats(), { reads: 1, saves: 1, deletes: 0 });
	} finally { delete (globalThis as any).eda; }
});

test('footprint precise reset readback retains sub-tenth-mil values before and after save', async () => {
	const host = mockHost({ quantizedGet: true });
	try {
		const res: any = await build({ topSolderMask: 1.968503937008, topPasteMask: 7.25, bottomPasteMask: 0 });
		assert.equal(res.result.verified, true);
		assert.deepEqual(host.stats(), { reads: 2, saves: 1, deletes: 0 });
	} finally { delete (globalThis as any).eda; }
});

test('footprint rejects unavailable or failed precise reset and reset identity drift', async () => {
	for (const options of [{ missingToAsync: true }, { missingReset: true }, { resetError: true }, { resetWrongId: true }]) {
		const host = mockHost(options);
		try { const res: any = await build({ topPasteMask: 0 }); assert.equal(res.result.verified, false); assert.equal(res.result.partial, true); assert.equal(host.stats().deletes, 1); }
		finally { delete (globalThis as any).eda; }
	}
});

test('footprint rejects precise reset loss, failure and identity drift occurring only after save', async () => {
	for (const options of [{ missingResetAtRead: 2 }, { resetErrorAtRead: 2 }, { resetWrongIdAtRead: 2 }]) {
		const host = mockHost({ ...options, quantizedGet: true });
		try {
			const res: any = await build({ topSolderMask: 1.968503937008, topPasteMask: 7.25 });
			assert.equal(res.result.verified, false);
			assert.equal(res.result.partial, true);
			assert.deepEqual(host.stats(), { reads: 2, saves: 1, deletes: 1 });
		} finally { delete (globalThis as any).eda; }
	}
});

test('footprint precise post-save comparison rejects drift exceeding one-millionth mil', async () => {
	const host = mockHost({ quantizedGet: true, read: (value, reads) => reads === 1 ? value : { ...value, topPasteMask: value.topPasteMask + 0.000002 } });
	try {
		const res: any = await build({ topSolderMask: 1.968503937008, topPasteMask: 7.25 });
		assert.equal(res.result.verified, false);
		assert.equal(res.result.partial, true);
		assert.match(res.result.error, /aperture topPasteMask mismatch/);
		assert.deepEqual(host.stats(), { reads: 2, saves: 1, deletes: 1 });
	} finally { delete (globalThis as any).eda; }
});

test('footprint rejects unknown, nonnumeric and nonfinite apertures before host mutation', async () => {
	for (const value of [0, [], { typo: 1 }, { topPasteMask: '0' }, { topPasteMask: false }, { topPasteMask: NaN }, { topPasteMask: Infinity }]) {
		const host = mockHost();
		try { await assert.rejects(() => build(value), (e: any) => e.code === 'PRECONDITION_REFUSED'); assert.equal(host.calls.length, 0); }
		finally { delete (globalThis as any).eda; }
	}
});

test('footprint stops at first dropped aperture instead of building all pads', async () => {
	const host = mockHost({ read: () => ({ topPasteMask: 1 }) });
	try { const res: any = await build({ topPasteMask: 0 }, 4); assert.equal(res.result.partial, true); assert.equal(res.result.verified, false); assert.equal(host.calls.length, 1); assert.deepEqual(host.stats(), { reads: 1, saves: 0, deletes: 1 }); }
	finally { delete (globalThis as any).eda; }
});

test('footprint catches aperture loss on save and wrong fresh primitive identity', async () => {
	for (const options of [{ read: (value: unknown, reads: number) => reads === 1 ? value : null }, { wrongId: true }]) {
		mockHost(options);
		try { const res: any = await build({ topPasteMask: 0 }); assert.equal(res.result.partial, true); assert.equal(res.result.verified, false); }
		finally { delete (globalThis as any).eda; }
	}
});

test('footprint refuses non-boolean or false save acknowledgement', async () => {
	for (const save of [false, undefined, 1, 'true']) {
		mockHost({ save });
		try { const res: any = await build({ topPasteMask: 0 }); assert.equal(res.result.partial, true); assert.equal(res.result.verified, false); assert.match(res.result.error, /save did not explicitly/); }
		finally { delete (globalThis as any).eda; }
	}
});
