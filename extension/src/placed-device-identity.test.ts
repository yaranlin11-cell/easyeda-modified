/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import test from 'node:test';

import {
	hydratePlacedIdentityCandidate,
	NativePlacedDeviceProof,
	NativePlacedDeviceSource,
	NativePlacedFootprintSource,
	PlacedIdentityReads,
	proveNativePlacedDevice,
} from './placed-device-identity';

type Row = Record<string, unknown>;
const DEVICE = 'a'.repeat(32), SECOND = 'b'.repeat(32), PERSONAL = 'c'.repeat(32);
const FOOTPRINT = 'd'.repeat(32), OTHER_FOOTPRINT = 'e'.repeat(32);
const SYSTEM_LIBRARY = 'f'.repeat(32), PERSONAL_LIBRARY = '0'.repeat(32);
const DEVICE_INSTANCE = '1'.repeat(16), FOOTPRINT_INSTANCE = '2'.repeat(16);
const SUPPLIER = 'C123456', MODEL = 'SYNTHETIC-PART-A', NAME = 'Synthetic device A';
const clone = <T>(value: T): T => structuredClone(value);
const deviceOrigin=(): NativePlacedDeviceSource=>({instanceUuid:DEVICE_INSTANCE,uuid:DEVICE,libraryUuid:SYSTEM_LIBRARY,footprintInstanceUuid:FOOTPRINT_INSTANCE,sourceKind:'project-epro2'});

test('native DEVICE origin uses one exact fresh detail and no shadowing search inventory',async()=>{
	const f=fixture(), h=readsFor(f,{queryError:'lcsc',lcscRows:[{uuid:SECOND,libraryUuid:SYSTEM_LIBRARY}],scopedRows:[{uuid:SECOND,libraryUuid:SYSTEM_LIBRARY}]});
	const proof=await proveNativePlacedDevice(f.snapshot,f.source,h.reads,deviceOrigin());
	assert.deepEqual(proof.device,{uuid:DEVICE,libraryUuid:SYSTEM_LIBRARY,via:'native-device-source'});
	assert.deepEqual(h.calls,[{kind:'get',args:[DEVICE,SYSTEM_LIBRARY]}]);
	assert.equal(h.contextChecks(),2);
});

test('native DEVICE owner, library and footprint association conflicts fail before any read',async t=>{
	const changes: Array<(source: NativePlacedDeviceSource)=>void>=[
		s=>{s.instanceUuid='9'.repeat(16);},s=>{s.uuid=DEVICE_INSTANCE;},s=>{s.libraryUuid=PERSONAL_LIBRARY;},s=>{s.footprintInstanceUuid='9'.repeat(16);},s=>{s.sourceKind='unproven' as any;},
	];
	for (const [i,change] of changes.entries()) await t.test(String(i),async()=>{
		const f=fixture(), h=readsFor(f), source=deviceOrigin(); change(source);
		unresolved(await proveNativePlacedDevice(f.snapshot,f.source,h.reads,source),/native DEVICE source conflicts/);
		assert.deepEqual(h.calls,[]);
	});
	const f=fixture(), h=readsFor(f); (f.snapshot.component as Row).uuid='9'.repeat(16);
	unresolved(await proveNativePlacedDevice(f.snapshot,f.source,h.reads,deviceOrigin()),/owner/); assert.deepEqual(h.calls,[]);
});

test('native DEVICE exact details remain fail closed on incomplete/conflicting identity and context drift',async t=>{
	const changes: Array<(detail: Row)=>void>=[
		d=>{delete d.association;},d=>{d.uuid=SECOND;},d=>{d.libraryUuid=PERSONAL_LIBRARY;},
		d=>{(d.property as Row).supplierId='C999999';},d=>{delete (d.property as Row).supplierId;},
		d=>{(d.property as Row).manufacturerId='OTHER';},d=>{delete (d.property as Row).manufacturerId;},
		d=>{((d.association as Row).footprint as Row).uuid=OTHER_FOOTPRINT;},d=>{((d.association as Row).footprint as Row).libraryUuid=PERSONAL_LIBRARY;},
	];
	for (const [i,change] of changes.entries()) await t.test(String(i),async()=>{
		const f=fixture(); change(f.detail); const h=readsFor(f);
		unresolved(await proveNativePlacedDevice(f.snapshot,f.source,h.reads,deviceOrigin()));
		assert.deepEqual(h.calls,[{kind:'get',args:[DEVICE,SYSTEM_LIBRARY]}]);
	});
	for (const at of [1,2]) await t.test(`context ${at}`,async()=>{
		const f=fixture(),h=readsFor(f,{contextFailureAt:at});
		unresolved(await proveNativePlacedDevice(f.snapshot,f.source,h.reads,deviceOrigin()),/context drift/);
	});
});

function fixture() {
	const snapshot: Row = {
		componentType: 'part', supplierId: SUPPLIER, manufacturerId: MODEL, name: NAME,
		device: { uuid: DEVICE_INSTANCE, libraryUuid: SYSTEM_LIBRARY, name: NAME },
		component: { uuid: DEVICE_INSTANCE, name: NAME },
		footprint: { uuid: FOOTPRINT_INSTANCE, libraryUuid: SYSTEM_LIBRARY, name: 'Placed footprint' },
	};
	const source: NativePlacedFootprintSource = {
		instanceUuid: FOOTPRINT_INSTANCE, uuid: FOOTPRINT, libraryUuid: SYSTEM_LIBRARY,
	};
	const row: Row = {
		uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY, supplierId: SUPPLIER, manufacturerId: MODEL, name: NAME,
		footprint: { uuid: FOOTPRINT, libraryUuid: SYSTEM_LIBRARY, name: 'Library footprint' },
	};
	const detail: Row = {
		uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY, name: NAME,
		property: { supplierId: SUPPLIER, manufacturerId: MODEL },
		association: { footprint: { uuid: FOOTPRINT, libraryUuid: SYSTEM_LIBRARY, name: 'Library footprint' } },
	};
	return { snapshot, source, row, detail };
}

interface ReadOptions {
	lcscRows?: unknown;
	scopedRows?: unknown;
	detailFor?: (uuid: string, library: string) => unknown | Promise<unknown>;
	contextFailureAt?: number;
	queryError?: 'lcsc' | 'search';
}

function readsFor(f: ReturnType<typeof fixture>, options: ReadOptions = {}) {
	const calls: Array<{ kind: string; args: string[] }> = [];
	let contextChecks = 0;
	const reads: PlacedIdentityReads = {
		byLcsc: async id => {
			calls.push({ kind: 'lcsc', args: [id] });
			if (options.queryError === 'lcsc') throw new Error('synthetic LCSC read failure');
			return options.lcscRows === undefined ? [clone(f.row)] : options.lcscRows;
		},
		search: async (query, library) => {
			calls.push({ kind: 'search', args: [query, library] });
			if (options.queryError === 'search') throw new Error('synthetic scoped read failure');
			return options.scopedRows === undefined ? [clone(f.row)] : options.scopedRows;
		},
		get: async (uuid, library) => {
			calls.push({ kind: 'get', args: [uuid, library] });
			return options.detailFor ? options.detailFor(uuid, library) : clone(f.detail);
		},
		assertContext: async () => {
			contextChecks++;
			if (options.contextFailureAt && contextChecks >= options.contextFailureAt) throw new Error('synthetic context drift');
		},
	};
	return { reads, calls, contextChecks: () => contextChecks };
}

function unresolved(proof: NativePlacedDeviceProof, reason?: RegExp) {
	assert.equal(proof.device, undefined);
	assert.ok(proof.reason, 'failed identity proof must explain the unresolved evidence');
	if (reason) assert.match(proof.reason!, reason);
}

test('sparse candidate details fill missing evidence without mutating either input', () => {
	const f = fixture(), row = { uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY };
	const beforeRow = clone(row), beforeDetail = clone(f.detail);
	const hydrated = hydratePlacedIdentityCandidate(row, f.detail);
	assert.deepEqual(hydrated.candidate, {
		...row, name: NAME, supplierId: SUPPLIER, manufacturerId: MODEL,
		footprint: { uuid: FOOTPRINT, libraryUuid: SYSTEM_LIBRARY, name: 'Library footprint' },
	});
	assert.equal(hydrated.error, undefined);
	assert.deepEqual(row, beforeRow);
	assert.deepEqual(f.detail, beforeDetail);
});

test('candidate hydration preserves nonempty fields and refuses every identity conflict', async t => {
	const changes: Array<[string, (f: ReturnType<typeof fixture>) => void]> = [
		['device uuid', f => { f.detail.uuid = SECOND; }],
		['device library', f => { f.detail.libraryUuid = PERSONAL_LIBRARY; }],
		['supplier', f => { (f.detail.property as Row).supplierId = 'C999999'; }],
		['manufacturer', f => { (f.detail.property as Row).manufacturerId = 'SYNTHETIC-OTHER'; }],
		['name', f => { f.detail.name = 'Synthetic conflicting device'; }],
		['footprint uuid', f => { ((f.detail.association as Row).footprint as Row).uuid = OTHER_FOOTPRINT; }],
		['footprint library', f => { ((f.detail.association as Row).footprint as Row).libraryUuid = PERSONAL_LIBRARY; }],
		['supplier alias', f => { delete f.row.supplierId; f.row.otherProperty = { 'Supplier Part': 'C999999' }; }],
		['manufacturer alias', f => { delete f.row.manufacturerId; f.row.otherProperty = { 'Manufacturer Part': 'SYNTHETIC-OTHER' }; }],
		['supplier canonical and alias disagree', f => { f.row.otherProperty = { 'Supplier Part': 'C999999' }; }],
		['manufacturer canonical and alias disagree', f => { f.row.otherProperty = { 'Manufacturer Part': 'SYNTHETIC-OTHER' }; }],
	];
	for (const [label, change] of changes) await t.test(label, () => {
		const f = fixture(); change(f);
		const before = clone(f.row), result = hydratePlacedIdentityCandidate(f.row, f.detail);
		assert.equal(result.candidate, undefined);
		assert.ok(result.error);
		assert.deepEqual(f.row, before);
	});
});

test('a matching alias cannot conceal conflicting canonical identity in the same row', async t => {
	for (const field of ['supplierId', 'manufacturerId'] as const) await t.test(field, async () => {
		const f = fixture(), row = clone(f.row);
		row.otherProperty = { [field === 'supplierId' ? 'Supplier Part' : 'Manufacturer Part']: row[field] };
		row[field] = field === 'supplierId' ? 'C999999' : 'SYNTHETIC-OTHER';
		const h = readsFor(f, { lcscRows: [row], scopedRows: [] });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /conflicts/);
		assert.equal(h.calls.filter(call => call.kind === 'get').length, 1);
	});
});

test('candidate hydration refuses incomplete or nonlibrary authoritative details', async t => {
	const changes: Array<[string, (f: ReturnType<typeof fixture>) => unknown]> = [
		['null detail', () => null], ['array detail', () => []],
		['missing device uuid', f => { delete f.detail.uuid; return f.detail; }],
		['missing device library', f => { delete f.detail.libraryUuid; return f.detail; }],
		['missing association', f => { delete f.detail.association; return f.detail; }],
		['placed footprint association', f => { ((f.detail.association as Row).footprint as Row).uuid = FOOTPRINT_INSTANCE; return f.detail; }],
		['nonhex footprint association', f => { ((f.detail.association as Row).footprint as Row).uuid = 'x'.repeat(32); return f.detail; }],
		['missing footprint library', f => { delete ((f.detail.association as Row).footprint as Row).libraryUuid; return f.detail; }],
	];
	for (const [label, change] of changes) await t.test(label, () => {
		const f = fixture(), result = hydratePlacedIdentityCandidate(f.row, change(f));
		assert.equal(result.candidate, undefined); assert.ok(result.error);
	});
});

test('sparse original-library rows are hydrated and scope is queried before unique resolution', async () => {
	const f = fixture(), before = clone(f.snapshot);
	const h = readsFor(f, { lcscRows: [], scopedRows: [{ uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY }] });
	const proof = await proveNativePlacedDevice(f.snapshot, f.source, h.reads);
	assert.deepEqual(proof.device, { uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY, via: 'lcsc-footprint-source' });
	assert.deepEqual(proof.footprintSource, f.source);
	assert.equal(proof.lcsc, SUPPLIER);
	assert.deepEqual(h.calls, [
		{ kind: 'lcsc', args: [SUPPLIER] }, { kind: 'search', args: [MODEL, SYSTEM_LIBRARY] },
		{ kind: 'get', args: [DEVICE, SYSTEM_LIBRARY] },
	]);
	assert.deepEqual(f.snapshot, before);
	assert.equal(h.contextChecks(), 7);
});

test('a complete LCSC match still queries the original library and deduplicates identical devices', async () => {
	const f = fixture(), h = readsFor(f);
	const proof = await proveNativePlacedDevice(f.snapshot, f.source, h.reads);
	assert.equal(proof.device?.uuid, DEVICE);
	assert.deepEqual(h.calls.filter(call => call.kind === 'search'), [{ kind: 'search', args: [MODEL, SYSTEM_LIBRARY] }]);
	assert.equal(h.calls.filter(call => call.kind === 'get').length, 1);
});

test('an original-library exact match survives a shadowing personal LCSC copy', async () => {
	const f = fixture();
	const h = readsFor(f, {
		lcscRows: [{ uuid: PERSONAL, libraryUuid: PERSONAL_LIBRARY, supplierId: SUPPLIER, manufacturerId: MODEL, name: NAME }],
		scopedRows: [{ uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY, name: NAME }],
		detailFor: (uuid, library) => uuid === DEVICE ? clone(f.detail) : {
			uuid, libraryUuid: library, name: NAME, property: { supplierId: SUPPLIER, manufacturerId: MODEL },
			association: { footprint: { uuid: OTHER_FOOTPRINT, libraryUuid: PERSONAL_LIBRARY } },
		},
	});
	const proof = await proveNativePlacedDevice(f.snapshot, f.source, h.reads);
	assert.equal(proof.device?.uuid, DEVICE);
	assert.equal(h.calls.filter(call => call.kind === 'get').length, 2);
});

test('a complete LCSC match cannot hide a second exact original-library device', async () => {
	const f = fixture(), second = { ...clone(f.row), uuid: SECOND };
	const h = readsFor(f, { scopedRows: [second], detailFor: uuid => ({ ...clone(f.detail), uuid }) });
	unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /ambiguous/);
});

test('exact identities from different device libraries remain ambiguous', async () => {
	const f = fixture(), personal = { ...clone(f.row), uuid: PERSONAL, libraryUuid: PERSONAL_LIBRARY };
	const h = readsFor(f, { lcscRows: [f.row, personal], detailFor: (uuid, libraryUuid) => ({ ...clone(f.detail), uuid, libraryUuid }) });
	unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /ambiguous/);
});

test('duplicate rows cannot conceal nonempty conflicting evidence', async t => {
	const changes: Array<[string, (row: Row) => void]> = [
		['footprint', row => { (row.footprint as Row).uuid = OTHER_FOOTPRINT; }],
		['footprint library', row => { (row.footprint as Row).libraryUuid = PERSONAL_LIBRARY; }],
		['name', row => { row.name = 'Synthetic duplicate conflict'; }],
		['manufacturer', row => { row.manufacturerId = 'SYNTHETIC-OTHER'; }],
		['supplier', row => { row.supplierId = 'C999999'; }],
	];
	for (const [label, change] of changes) await t.test(label, async () => {
		const f = fixture(), conflicting = clone(f.row); change(conflicting);
		const h = readsFor(f, { scopedRows: [conflicting] });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
		assert.equal(h.calls.filter(call => call.kind === 'get').length, 1, 'authoritative detail can be shared, duplicate evidence must still be checked');
	});
});

test('missing native source, wrong owner, and mismatched source domain fail before API reads', async t => {
	const changes: Array<[string, (f: ReturnType<typeof fixture>) => void]> = [
		['missing source asset', f => { f.source.uuid = ''; }],
		['16hex source asset', f => { f.source.uuid = FOOTPRINT_INSTANCE; }],
		['nonhex source asset', f => { f.source.uuid = 'x'.repeat(32); }],
		['missing owner', f => { f.source.instanceUuid = ''; }],
		['different owner', f => { f.source.instanceUuid = '3'.repeat(16); }],
		['library-shaped owner', f => { f.source.instanceUuid = FOOTPRINT; (f.snapshot.footprint as Row).uuid = FOOTPRINT; }],
		['different library', f => { f.source.libraryUuid = PERSONAL_LIBRARY; }],
		['missing placed library', f => { delete (f.snapshot.device as Row).libraryUuid; }],
		['missing exact query', f => {
			f.snapshot.manufacturerId = ''; f.snapshot.name = '={Manufacturer Part}';
			(f.snapshot.component as Row).name = '={Manufacturer Part}'; (f.snapshot.device as Row).name = '={Manufacturer Part}';
		}],
	];
	for (const [label, change] of changes) await t.test(label, async () => {
		const f = fixture(); change(f); const h = readsFor(f);
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
		assert.equal(h.calls.length, 0);
	});
});

function customFixture(useName: boolean) {
	const f = fixture(); f.snapshot.supplierId = ''; f.snapshot.manufacturerId = useName ? '' : MODEL;
	(f.snapshot.device as Row).libraryUuid = PERSONAL_LIBRARY;
	(f.snapshot.footprint as Row).libraryUuid = PERSONAL_LIBRARY; f.source.libraryUuid = PERSONAL_LIBRARY;
	f.row.libraryUuid = PERSONAL_LIBRARY; f.row.supplierId = ''; f.row.manufacturerId = useName ? '' : MODEL;
	(f.row.footprint as Row).libraryUuid = PERSONAL_LIBRARY;
	f.detail.libraryUuid = PERSONAL_LIBRARY; (f.detail.property as Row).supplierId = '';
	(f.detail.property as Row).manufacturerId = useName ? '' : MODEL;
	((f.detail.association as Row).footprint as Row).libraryUuid = PERSONAL_LIBRARY;
	return f;
}

test('supplierless custom parts use exact MPN or stable name in their original library only', async t => {
	for (const useName of [false, true]) await t.test(useName ? 'stable name' : 'manufacturer part', async () => {
		const f = customFixture(useName), h = readsFor(f, { scopedRows: [{ uuid: DEVICE, libraryUuid: PERSONAL_LIBRARY }] });
		const proof = await proveNativePlacedDevice(f.snapshot, f.source, h.reads);
		assert.deepEqual(proof.device, { uuid: DEVICE, libraryUuid: PERSONAL_LIBRARY, via: 'scoped-footprint-source' });
		assert.equal(proof.lcsc, undefined);
		assert.equal(h.calls.filter(call => call.kind === 'lcsc').length, 0);
		assert.deepEqual(h.calls.find(call => call.kind === 'search')?.args, [useName ? NAME : MODEL, PERSONAL_LIBRARY]);
	});
});

test('supplierless custom parts refuse nonempty supplier identities or missing fresh stable name', async t => {
	for (const scenario of ['search-and-detail-supplier', 'detail-only-supplier', 'missing-detail-name']) await t.test(scenario, async () => {
		const f = customFixture(true);
		if (scenario === 'missing-detail-name') delete f.detail.name;
		else {
			(f.detail.property as Row).supplierId = SUPPLIER;
			if (scenario === 'search-and-detail-supplier') f.row.supplierId = SUPPLIER;
		}
		const h = readsFor(f);
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
	});
});

test('a malformed nonempty supplier cannot silently enter the supplierless custom path', async () => {
	const f = customFixture(false); f.snapshot.supplierId = 'not-a-C-number'; const h = readsFor(f);
	unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
	assert.equal(h.calls.length, 0);
});

test('wrong-library scoped results cannot establish a custom identity', async () => {
	const f = customFixture(false), wrong = { ...clone(f.row), libraryUuid: SYSTEM_LIBRARY };
	const h = readsFor(f, { scopedRows: [wrong] });
	unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
	assert.equal(h.calls.filter(call => call.kind === 'get').length, 0);
});

test('distinct supplierless original-library devices sharing a native source remain ambiguous', async () => {
	const f = customFixture(false), second = { ...clone(f.row), uuid: SECOND };
	const h = readsFor(f, { scopedRows: [f.row, second], detailFor: uuid => ({ ...clone(f.detail), uuid }) });
	unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /ambiguous/);
	assert.equal(h.calls.filter(call => call.kind === 'lcsc').length, 0);
});

test('missing exact model, supplier, or footprint source cannot be guessed from a sparse candidate', async t => {
	for (const scenario of ['supplier', 'manufacturer', 'footprint']) await t.test(scenario, async () => {
		const f = fixture();
		if (scenario === 'footprint') ((f.detail.association as Row).footprint as Row).uuid = OTHER_FOOTPRINT;
		else delete (f.detail.property as Row)[scenario === 'supplier' ? 'supplierId' : 'manufacturerId'];
		const h = readsFor(f, { lcscRows: [], scopedRows: [{ uuid: DEVICE, libraryUuid: SYSTEM_LIBRARY }] });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
	});
});

test('malformed candidate inventories and raw limits fail before detail reads', async t => {
	const f = fixture();
	const cases: Array<[string, ReadOptions]> = [
		['LCSC nonarray', { lcscRows: {} }], ['scoped nonarray', { scopedRows: null }],
		['129 LCSC rows', { lcscRows: Array.from({ length: 129 }, () => clone(f.row)) }],
		['129 scoped rows', { scopedRows: Array.from({ length: 129 }, () => clone(f.row)) }],
		['129 distinct merged rows', {
			lcscRows: Array.from({ length: 65 }, (_, i) => ({ ...clone(f.row), uuid: (i + 1).toString(16).padStart(32, '0') })),
			scopedRows: Array.from({ length: 64 }, (_, i) => ({ ...clone(f.row), uuid: (i + 66).toString(16).padStart(32, '0') })),
		}],
	];
	for (const [label, options] of cases) await t.test(label, async () => {
		const h = readsFor(f, options); unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
		assert.equal(h.calls.filter(call => call.kind === 'get').length, 0);
	});
});

test('exactly 128 duplicate rows are bounded and reuse one authoritative detail', async () => {
	const f = fixture(), h = readsFor(f, { lcscRows: Array.from({ length: 128 }, () => clone(f.row)), scopedRows: [] });
	assert.equal((await proveNativePlacedDevice(f.snapshot, f.source, h.reads)).device?.uuid, DEVICE);
	assert.equal(h.calls.filter(call => call.kind === 'get').length, 1);
});

test('a plausible malformed candidate blocks success instead of disappearing beside a valid device', async t => {
	for (const candidate of [null, [], {}, { uuid: DEVICE_INSTANCE, libraryUuid: SYSTEM_LIBRARY }, { uuid: 'x'.repeat(32), libraryUuid: SYSTEM_LIBRARY }, { uuid: SECOND }]) {
		await t.test(JSON.stringify(candidate), async () => {
			const f = fixture(), h = readsFor(f, { lcscRows: [f.row, candidate] });
			unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
		});
	}
});

test('malformed scoped-library evidence cannot disappear beside a complete LCSC match', async t => {
	for (const library of [undefined, '', 'invalid scope']) await t.test(String(library), async () => {
		const f = fixture(), h = readsFor(f, { scopedRows: [{ uuid: SECOND, libraryUuid: library }] });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /library device identity/);
	});
});

test('candidate query failures cannot use only the remaining inventory', async t => {
	for (const queryError of ['lcsc', 'search'] as const) await t.test(queryError, async () => {
		const f = fixture(), h = readsFor(f, { queryError });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /query failed/);
		assert.equal(h.calls.filter(call => call.kind === 'get').length, 0);
	});
});

test('a failed detail read or wrong returned identity blocks success even beside a complete match', async t => {
	for (const scenario of ['throws', 'null', 'wrong-uuid', 'wrong-library']) await t.test(scenario, async () => {
		const f = fixture(), second = { ...clone(f.row), uuid: SECOND };
		const h = readsFor(f, { scopedRows: [second], detailFor: (uuid, library) => {
			if (uuid === DEVICE) return clone(f.detail);
			if (scenario === 'throws') throw new Error('synthetic detail failure');
			if (scenario === 'null') return null;
			return { ...clone(f.detail), uuid: scenario === 'wrong-uuid' ? PERSONAL : uuid, libraryUuid: scenario === 'wrong-library' ? PERSONAL_LIBRARY : library };
		} });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads));
	});
});

test('context drift before or after every external read, and at final verification, blocks proof', async t => {
	for (let at = 1; at <= 7; at++) await t.test(`context check ${at}`, async () => {
		const f = fixture(), before = clone(f.snapshot), h = readsFor(f, { contextFailureAt: at });
		unresolved(await proveNativePlacedDevice(f.snapshot, f.source, h.reads), /context drift/);
		assert.deepEqual(f.snapshot, before);
		if (at === 1) assert.equal(h.calls.length, 0);
	});
});
