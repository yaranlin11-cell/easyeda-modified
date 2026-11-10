/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test, type TestContext } from 'node:test';
import { runAction, samePhysicalPinGeometry } from './actions';
import JSZip from 'jszip';

type State = Record<string, unknown>;
type Ref = { uuid: string; libraryUuid: string };
type Event = { kind: string; documentUuid: string; args: unknown[] };
const DEVICE = 'a'.repeat(32), DEVICE_LIBRARY = 'b'.repeat(32);
const OLD_ASSET = 'c'.repeat(32), NEW_ASSET = 'd'.repeat(32), OTHER_ASSET = '9'.repeat(32);
const PERSONAL_LIBRARY = 'e'.repeat(32), CLONE_DEVICE = 'f'.repeat(32), SECOND_DEVICE = '0'.repeat(32);
const PROJECT = '1234567890abcdef'.repeat(2), PAGE = '1234567890abcdef', OTHER_PAGE = 'fedcba0987654321';
const OLD_ID = '1'.repeat(16), OLD_DEVICE_INSTANCE = '2'.repeat(16), OLD_FP_INSTANCE = '3'.repeat(16);
const CANDIDATE_ID = '4'.repeat(16), CANDIDATE_DEVICE_INSTANCE = '5'.repeat(16), CANDIDATE_FP_INSTANCE = '6'.repeat(16);
const RESTORED_ID = '7'.repeat(16), RESTORED_DEVICE_INSTANCE = '8'.repeat(16), RESTORED_FP_INSTANCE = '9'.repeat(16);
const NAME = 'Synthetic rebind device', MODEL = 'SYNTHETIC-REBIND-A', SUPPLIER = 'C123456';
const TARGET_NAME = 'Synthetic target footprint', CLONE_NAME = `${NAME}_${TARGET_NAME}`;
const OLD_REF: Ref = { uuid: OLD_ASSET, libraryUuid: DEVICE_LIBRARY };
const NEW_REF: Ref = { uuid: NEW_ASSET, libraryUuid: DEVICE_LIBRARY };
const clone = <T>(value: T): T => structuredClone(value);
const mutationKinds = new Set(['device.modify', 'device.copy', 'device.delete', 'component.create', 'component.modify', 'component.delete']);

function primitive(state: State): any {
	const fields: Record<string, string> = {
		PrimitiveId: 'primitiveId', ComponentType: 'componentType', Designator: 'designator', Name: 'name',
		X: 'x', Y: 'y', Rotation: 'rotation', Mirror: 'mirror', Net: 'net', SubPartName: 'subPartName',
		AddIntoBom: 'addIntoBom', AddIntoPcb: 'addIntoPcb', UniqueId: 'uniqueId',
		Manufacturer: 'manufacturer', ManufacturerId: 'manufacturerId', Supplier: 'supplier', SupplierId: 'supplierId',
		Component: 'device', Symbol: 'symbol', Footprint: 'footprint', OtherProperty: 'otherProperty',
	};
	return Object.fromEntries(Object.entries(fields).map(([method, key]) => [`getState_${method}`, () => state[key]]));
}

interface HarnessOptions {
 initialBinding?: boolean;
 copiedSoleSymbol?: boolean;
 metadataOnlyFlipsPins?: boolean;
 nativeUnit?: string;
 invertCreateRotation?: boolean;
	prepared?: boolean;
	ignoreCloneAssociation?: boolean;
	falseCloneAssociation?: boolean;
	renamedUnit?: boolean;
	ambiguousUnits?: boolean;
	changedPinGeometry?: boolean;
	droppedPinNC?: boolean;
	exactNativeDevice?: boolean;
	exactMissingAssociation?: boolean;
	custom?: 'mpn' | 'name';
	cloneFallback?: boolean;
	mismatchUniqueId?: boolean;
	wrongCandidateSource?: boolean;
	missingRollbackSource?: boolean;
	preflight?: 'missing-source' | 'duplicate-source' | 'wrong-owner' | 'wrong-source-asset' | 'ambiguous' | 'conflict' | 'query-error';
	drift?: 'native-source' | 'scope-query' | 'preflight-association' | 'device-modify' | 'candidate-create' | 'instance-restore';
}

function harness(options: HarnessOptions = {}) {
	let currentPage = PAGE, createCount = 0, originalGets = 0;
	const events: Event[] = [];
	const associations = new Map<string, Ref>([[DEVICE, clone(OLD_REF)], [CLONE_DEVICE, clone(OLD_REF)]]);
	if (options.prepared) associations.set(CLONE_DEVICE, clone(NEW_REF));
	if (options.initialBinding) associations.set(DEVICE, {uuid:'', libraryUuid:DEVICE_LIBRARY});
	const originalState = (): State => ({
		primitiveId: OLD_ID, componentType: 'part', designator: 'U3', name: NAME,
		x: 1350, y: 1020, rotation: 90, mirror: true, net: '', subPartName: options.initialBinding ? 'abababababababab' : options.renamedUnit ? 'Original.1' : '',
		addIntoBom: true, addIntoPcb: true, uniqueId: 'synthetic-stable-link',
		manufacturer: 'Synthetic vendor', manufacturerId: options.custom === 'name' ? '' : MODEL,
		supplier: options.custom ? '' : 'LCSC', supplierId: options.custom ? '' : SUPPLIER,
		device: { uuid: OLD_DEVICE_INSTANCE, libraryUuid: DEVICE_LIBRARY, name: NAME },
		symbol: { uuid: 'abcdef0123456789', libraryUuid: DEVICE_LIBRARY },
		footprint: options.initialBinding ? {uuid:''} : { uuid: OLD_FP_INSTANCE, libraryUuid: DEVICE_LIBRARY, name: 'Synthetic old package' },
		otherProperty: { Voltage: '3.3V', Purpose: 'synthetic fixture' },
		nativeSource: clone(OLD_REF),
	});
	const states = new Map<string, State>([[OLD_ID, originalState()]]);
	const log = (kind: string, ...args: unknown[]) => events.push({ kind, documentUuid: currentPage, args: clone(args) });
	const drift = (when: HarnessOptions['drift']) => {
		if (options.drift === when && currentPage === PAGE) { currentPage = OTHER_PAGE; log('context.drift', when); }
	};
	const detail = (uuid: string, libraryUuid: string): State | undefined => {
		if (![DEVICE, CLONE_DEVICE, SECOND_DEVICE].includes(uuid)) return undefined;
		return {
			uuid, libraryUuid, name: uuid === CLONE_DEVICE ? CLONE_NAME : NAME,
			property: { ...(options.initialBinding ? {manufacturer:'Synthetic vendor'} : {}), supplierId: options.custom ? '' : SUPPLIER, manufacturerId: options.custom === 'name' ? '' : MODEL },
			subPartNames: options.copiedSoleSymbol ? ['Original.1'] : uuid === CLONE_DEVICE ? (options.ambiguousUnits ? ['Clone.1', 'Clone.2'] : ['Clone.1']) : ['Original.1'],
			...(options.exactMissingAssociation ? {} : { association: { ...(options.initialBinding ? {symbol:{uuid: options.copiedSoleSymbol && uuid === CLONE_DEVICE ? 'bcde'.repeat(8) : 'abcd'.repeat(8),libraryUuid:DEVICE_LIBRARY}} : {}), footprint: clone(associations.get(uuid) ?? associations.get(DEVICE)!) } }),
		};
	};
	const scopedRow = (uuid: string, libraryUuid: string): State => ({ uuid, libraryUuid, name: uuid === CLONE_DEVICE ? CLONE_NAME : NAME });
	const sourceEntry = (state: State): State => {
		const footprint = state.footprint as Ref, source = state.nativeSource as Ref;
		return {
			footprintUuid: footprint.uuid,
			documentSource: [
				JSON.stringify({ type: 'DOCHEAD' }) + '||' + JSON.stringify({ docType: 'FOOTPRINT', uuid: footprint.uuid }) + '|',
				JSON.stringify({ type: 'META' }) + '||' + JSON.stringify({ title: 'Synthetic native footprint', source: `${source.uuid}|${source.libraryUuid}` }) + '|',
			].join('\n'),
		};
	};
	const pinNC = new Map<string, boolean>([[OLD_ID, true]]);
	const pin = (id: string): any => ({
		getState_PrimitiveId: () => id + '-pin', getState_PinNumber: () => '1', getState_PinName: () => 'IO',
		getState_X: () => id === CANDIDATE_ID && options.changedPinGeometry ? 1360 : 1350,
		getState_Y: () => states.get(id)?.flippedPins ? -1020 : 1020, getState_Rotation: () => options.invertCreateRotation ? Number(states.get(id)?.rotation) - 90 : 0, getState_NoConnected: () => pinNC.get(id) ?? false,
	});

	const eda: any = {
		sys_Log: { add: () => undefined },
		dmt_Project: { getCurrentProjectInfo: async () => ({ uuid: PROJECT }) },
		dmt_SelectControl: { getCurrentDocumentInfo: async () => ({ uuid: currentPage, tabId: `${currentPage}@${PROJECT}`, documentType: 1 }) },
		sys_FileManager: { getDocumentFootprintSources: async () => {
			let entries = [...states.values()].filter(state => !(options.missingRollbackSource && state.primitiveId === RESTORED_ID)).map(sourceEntry);
			if (!events.some(event => mutationKinds.has(event.kind))) {
				if (options.preflight === 'missing-source') entries = [];
				if (options.preflight === 'duplicate-source') entries.push(clone(entries[0]));
				if (options.preflight === 'wrong-owner') entries[0].documentSource = String(entries[0].documentSource).replace(OLD_FP_INSTANCE, CANDIDATE_FP_INSTANCE);
				if (options.preflight === 'wrong-source-asset') entries[0].documentSource = String(entries[0].documentSource).replace(OLD_ASSET, OTHER_ASSET);
			}
			log('native.sources', entries); drift('native-source'); return options.exactNativeDevice || options.initialBinding ? [] : entries;
		}, getProjectFile: async () => {
			if (!options.exactNativeDevice && !options.initialBinding) throw new Error('synthetic archive export is unavailable in document-source fixtures');
			const rows: string[] = [];
			for (const state of states.values()) {
				if ((state.footprint as Ref).uuid) rows.push(String(sourceEntry(state).documentSource));
				const device = state.device as Ref, footprint = state.footprint as Ref;
				rows.push(JSON.stringify({ type: 'DOCHEAD' }) + '||' + JSON.stringify({ docType: 'DEVICE', uuid: device.uuid }) + '|');
				rows.push(JSON.stringify({ type: 'META' }) + '||' + JSON.stringify({ source: options.initialBinding && state.primitiveId === CANDIDATE_ID ? `${CLONE_DEVICE}|${PERSONAL_LIBRARY}` : `${DEVICE}|${DEVICE_LIBRARY}`, attributes: { Footprint: footprint.uuid, ...(options.initialBinding ? {Symbol:(state.symbol as Ref).uuid} : {}) } }) + '|');
			}
			rows.push(JSON.stringify({ type: 'DOCHEAD' }) + '||' + JSON.stringify({ docType: 'SCH_PAGE', uuid: currentPage }) + '|');
			log('native.project', rows);
			const zip = new JSZip(); zip.file('synthetic.epru', rows.join('\n'));
			return new Blob([await zip.generateAsync({ type: 'arraybuffer' })]);
		} },
		lib_LibrariesList: { getPersonalLibraryUuid: async () => PERSONAL_LIBRARY },
		lib_Device: {
			getByLcscIds: async (ids: string[], _unused: unknown, all: boolean) => {
				log('device.byLcsc', ids, all); assert.deepEqual(ids, [SUPPLIER]); assert.equal(all, true);
				if (options.exactNativeDevice) throw new Error('exact native DEVICE source must not query LCSC candidates');
				return [scopedRow(DEVICE, DEVICE_LIBRARY)];
			},
			search: async (query: string, library: string) => {
				log('device.search', query, library); drift('scope-query');
				if (options.exactNativeDevice) throw new Error('exact native DEVICE source must not search candidates');
				if (options.preflight === 'query-error') throw new Error('synthetic scoped read failure');
				if (library === PERSONAL_LIBRARY) return [scopedRow(CLONE_DEVICE, PERSONAL_LIBRARY)];
				assert.equal(library, DEVICE_LIBRARY);
				assert.equal(query, options.custom === 'name' ? NAME : MODEL);
				const row = scopedRow(DEVICE, DEVICE_LIBRARY);
				if (options.preflight === 'conflict') row.footprint = { uuid: OTHER_ASSET, libraryUuid: DEVICE_LIBRARY };
				return options.preflight === 'ambiguous' ? [row, scopedRow(SECOND_DEVICE, DEVICE_LIBRARY)] : [row];
			},
			get: async (uuid: string, library: string) => {
				log('device.get', uuid, library);
				if (uuid === DEVICE && ++originalGets === 2) drift('preflight-association');
				return detail(uuid, library);
			},
			modify: async (uuid: string, library: string, _name: unknown, _description: unknown, update: { footprint?: Ref }) => {
				log('device.modify', uuid, library, update);
				if (options.cloneFallback && uuid === DEVICE) return false;
				if (options.ignoreCloneAssociation && uuid === CLONE_DEVICE) return !options.falseCloneAssociation;
				assert.ok(update.footprint); associations.set(uuid, clone(update.footprint!));
				drift('device-modify'); return true;
			},
			copy: async (...args: unknown[]) => { log('device.copy', ...args); throw new Error('existing synthetic clone should be reused'); },
			delete: async (...args: unknown[]) => { log('device.delete', ...args); return true; },
		},
		sch_PrimitivePin: { modify: async (id: string, patch: any) => { if (!options.droppedPinNC) pinNC.set(id.replace('-pin', ''), patch.noConnected); } },
		sch_PrimitiveComponent: {
			getAllPinsByPrimitiveId: async (id: string) => states.has(id) ? [pin(id)] : [],
			get: async (id: string) => { log('component.get', id); const state = states.get(id); return state ? primitive(state) : undefined; },
			getAll: async () => [...states.values()].map(primitive),
			create: async (device: Ref, x: number, y: number, subPartName: string, rotation: number, mirror: boolean, addIntoBom: boolean, addIntoPcb: boolean) => {
				createCount++; const candidate = createCount === 1;
				log('component.create', device, candidate ? CANDIDATE_ID : RESTORED_ID, subPartName);
				const association = associations.get(device.uuid)!;
				const state: State = {
					...originalState(), primitiveId: candidate ? CANDIDATE_ID : RESTORED_ID,
					name: device.uuid === CLONE_DEVICE ? CLONE_NAME : NAME, designator: 'U?', uniqueId: `synthetic-generated-${createCount}`,
					x, y, subPartName: options.initialBinding && !candidate ? 'efefefefefefefef' : candidate && options.nativeUnit ? options.nativeUnit : subPartName ?? (device.uuid === CLONE_DEVICE ? 'Clone.1' : 'Original.1'), rotation: options.invertCreateRotation ? (360 - rotation) % 360 : rotation, mirror, addIntoBom, addIntoPcb,
					manufacturer: '', manufacturerId: '', supplier: '', supplierId: '', otherProperty: {},
					device: { uuid: candidate ? CANDIDATE_DEVICE_INSTANCE : RESTORED_DEVICE_INSTANCE, libraryUuid: device.libraryUuid, name: device.uuid === CLONE_DEVICE ? CLONE_NAME : NAME },
					footprint: options.initialBinding && !candidate ? {uuid:''} : { uuid: candidate ? CANDIDATE_FP_INSTANCE : RESTORED_FP_INSTANCE, libraryUuid: association.libraryUuid, name: 'Synthetic placed package' },
					nativeSource: candidate && options.wrongCandidateSource ? clone(OLD_REF) : clone(association),
				};
				states.set(String(state.primitiveId), state); drift('candidate-create'); return primitive(state);
			},
			modify: async (id: string, properties: State) => {
				log('component.modify', id, properties); const state = states.get(id);
				if (!state) return undefined;
				if (options.metadataOnlyFlipsPins && id === CANDIDATE_ID) state.flippedPins = !('x' in properties && 'y' in properties);
				for (const [key, value] of Object.entries(properties)) {
					if (options.mismatchUniqueId && id === CANDIDATE_ID && key === 'uniqueId') continue;
					state[key] = clone(value);
				}
				drift('instance-restore'); return primitive(state);
			},
			delete: async (id: string) => { log('component.delete', id); states.delete(id); return true; },
		},
	};
	return { eda, events, states, associations };
}

function install(t: TestContext, mock: any) {
	const globals = globalThis as any;
	const keys = ['eda', 'EDMT_EditorDocumentType', 'document', 'MutationObserver', 'window'] as const;
	const previous = Object.fromEntries(keys.map(key => [key, globals[key]]));
	t.after(() => { for (const key of keys) { if (previous[key] === undefined) delete globals[key]; else globals[key] = previous[key]; } });
	globals.eda = mock;
	const hostDoc = { uuid: PAGE, tabId: `${PAGE}@${PROJECT}`, profileSetting: { readonlyMode: false } };
	globals.window = { SCH: { app: { actionRunner: mock.testRunner ?? { running: false, currentAction: null } }, docMemoryManager: { getActiveDoc: () => hostDoc } } };
	globals.EDMT_EditorDocumentType = { HOME: -1, BLANK: 0, SCHEMATIC_PAGE: 1, PCB: 3 };
	globals.document = { body: {}, querySelectorAll: () => [] };
	globals.MutationObserver = class { observe() {} disconnect() {} };
}

async function rebind(t: TestContext, f: ReturnType<typeof harness>, prepared = false): Promise<any> {
	install(t, f.eda);
	return runAction('schematic.rebind.footprint', {
		primitiveId: OLD_ID, footprint: TARGET_NAME, footprintUuid: NEW_ASSET, footprintLibraryUuid: DEVICE_LIBRARY,
		...(prepared ? { preparedDevice: { uuid: CLONE_DEVICE, libraryUuid: PERSONAL_LIBRARY } } : {}),
	});
}

function facts(error: any): any {
	const text = String(error.detail); const start = text.indexOf('{');
	assert.ok(start >= 0, text); return JSON.parse(text.slice(start));
}

test('native16 rebind proves sparse original and fresh replacement against their separate32 sources', async t => {
	const f = harness(), result = await rebind(t, f);
	assert.equal(result.result.mode, 'in-place'); assert.equal(result.result.verified, true);
	assert.equal(result.mutationContextAvailable, true);
	assert.deepEqual(result.result.originalPlacedReference, { uuid: OLD_FP_INSTANCE, libraryUuid: DEVICE_LIBRARY, name: 'Synthetic old package' });
	assert.deepEqual(result.result.originalLibraryReference, OLD_REF);
	assert.deepEqual(result.result.device, { uuid: DEVICE, libraryUuid: DEVICE_LIBRARY });
	assert.equal(result.result.component.footprint.uuid, CANDIDATE_FP_INSTANCE);
	assert.equal(result.result.component.uniqueId, 'synthetic-stable-link');
	assert.equal(f.states.has(OLD_ID), false); assert.equal(f.states.has(CANDIDATE_ID), true);
	const firstMutation = f.events.findIndex(event => mutationKinds.has(event.kind));
	assert.ok(f.events.slice(0, firstMutation).some(event => event.kind === 'device.search' && event.args[1] === DEVICE_LIBRARY));
	const oldDelete = f.events.findIndex(event => event.kind === 'component.delete' && event.args[0] === OLD_ID);
	assert.ok(f.events.slice(0, oldDelete).some(event => event.kind === 'native.sources' && JSON.stringify(event.args).includes(NEW_ASSET)), 'fresh candidate native source must be reviewed before deleting the original');
	const finalSource = f.events.filter(event => event.kind === 'native.sources').at(-1)!;
	assert.match(JSON.stringify(finalSource.args), new RegExp(CANDIDATE_FP_INSTANCE));
	assert.match(JSON.stringify(finalSource.args), new RegExp(NEW_ASSET));
});

test('native16 rebind uses exact native DEVICE origin without querying unrelated candidates', async t => {
	const f = harness({ exactNativeDevice: true }), result = await rebind(t, f);
	assert.equal(result.result.mode, 'in-place'); assert.equal(result.result.verified, true);
	assert.deepEqual(result.result.originalLibraryReference, OLD_REF);
	assert.deepEqual(result.result.device, { uuid: DEVICE, libraryUuid: DEVICE_LIBRARY });
	assert.equal(result.result.component.footprint.uuid, CANDIDATE_FP_INSTANCE);
	assert.equal(result.result.component.uniqueId, 'synthetic-stable-link');
	assert.equal(f.states.has(OLD_ID), false); assert.equal(f.states.has(CANDIDATE_ID), true);
	assert.equal(f.events.some(event => ['device.byLcsc', 'device.search'].includes(event.kind)), false);
	const firstMutation = f.events.findIndex(event => mutationKinds.has(event.kind));
	const preflightGets = f.events.slice(0, firstMutation).filter(event => event.kind === 'device.get');
	assert.equal(preflightGets.length, 2, 'one exact identity get followed by the existing association guard');
	assert.ok(preflightGets.every(event => event.args[0] === DEVICE && event.args[1] === DEVICE_LIBRARY));
	const oldDelete = f.events.findIndex(event => event.kind === 'component.delete' && event.args[0] === OLD_ID);
	assert.ok(f.events.slice(firstMutation, oldDelete).some(event => event.kind === 'native.project' && JSON.stringify(event.args).includes(CANDIDATE_FP_INSTANCE)
		&& JSON.stringify(event.args).includes(NEW_ASSET)), 'fresh candidate native source must be reviewed before deleting the original');
});

test('native16 exact native DEVICE detail without an association refuses before every mutation', async t => {
	const f = harness({ exactNativeDevice: true, exactMissingAssociation: true });
	await assert.rejects(() => rebind(t, f), (error: any) => {
		assert.match(`${error.message} ${error.detail}`, /native DEVICE device\.get.*association/i); return true;
	});
	assert.equal(f.events.some(event => mutationKinds.has(event.kind)), false);
	assert.equal(f.events.some(event => ['device.byLcsc', 'device.search'].includes(event.kind)), false);
	assert.deepEqual(f.events.filter(event => event.kind === 'device.get').map(event => event.args), [[DEVICE, DEVICE_LIBRARY]]);
	assert.equal(f.states.has(OLD_ID), true); assert.deepEqual(f.associations.get(DEVICE), OLD_REF);
});

test('native16 uniqueId failure rolls library back to the proven32 asset and freshly verifies restored original', async t => {
	const f = harness({ mismatchUniqueId: true });
	await assert.rejects(() => rebind(t, f), (error: any) => {
		const report = facts(error); assert.equal(report.phase, 'restore-and-verify');
		assert.equal(report.rollback.verified, true); assert.equal(report.rollback.restoredOriginal.primitiveId, RESTORED_ID);
		assert.match(report.cause, /uniqueId/); return true;
	});
	const updates = f.events.filter(event => event.kind === 'device.modify');
	assert.deepEqual(updates.map(event => (event.args[2] as any).footprint), [NEW_REF, OLD_REF]);
	assert.ok(updates.every(event => (event.args[2] as any).footprint.uuid.length === 32));
	assert.equal(f.states.has(CANDIDATE_ID), false); assert.equal(f.states.has(RESTORED_ID), true);
	assert.equal(f.states.get(RESTORED_ID)?.uniqueId, 'synthetic-stable-link');
	const rollbackWrite = f.events.findIndex(event => event.kind === 'device.modify' && (event.args[2] as any).footprint.uuid === OLD_ASSET);
	const rollbackReads = f.events.slice(rollbackWrite + 1);
	assert.ok(rollbackReads.some(event => event.kind === 'native.sources' && JSON.stringify(event.args).includes(RESTORED_FP_INSTANCE)));
	assert.ok(rollbackReads.some(event => event.kind === 'device.search' && event.args[0] === MODEL && event.args[1] === DEVICE_LIBRARY));
	assert.ok(rollbackReads.some(event => event.kind === 'device.get' && event.args[0] === DEVICE));
});

test('native16 replacement with a wrong source is rejected before deleting the original', async t => {
	const f = harness({ wrongCandidateSource: true });
	await assert.rejects(() => rebind(t, f), (error: any) => {
		const report = facts(error); assert.equal(report.phase, 'candidate-binding-readback');
		assert.equal(report.rollback.verified, true); return true;
	});
	assert.equal(f.events.some(event => event.kind === 'component.delete' && event.args[0] === OLD_ID), false);
	assert.equal(f.states.has(OLD_ID), true); assert.equal(f.states.has(CANDIDATE_ID), false);
	assert.deepEqual(f.associations.get(DEVICE), OLD_REF);
});

test('native16 rollback lacking restored native evidence remains unverified and exposes its new primitive', async t => {
	const f = harness({ mismatchUniqueId: true, missingRollbackSource: true });
	await assert.rejects(() => rebind(t, f), (error: any) => {
		const report = facts(error); assert.equal(report.rollback.verified, false);
		assert.equal(report.rollback.restoredOriginal.primitiveId, RESTORED_ID);
		assert.equal(report.rollback.restoredOriginal.verified, false);
		assert.match(report.rollback.errors.join('; '), /original instance restore|native|identity/i); return true;
	});
	assert.equal(f.states.has(RESTORED_ID), true); assert.equal(f.states.has(CANDIDATE_ID), false);
	assert.deepEqual(f.associations.get(DEVICE), OLD_REF);
});

test('native16 missing, ambiguous, conflicting, and incomplete preflight evidence refuses every mutation', async t => {
	for (const preflight of ['missing-source', 'duplicate-source', 'wrong-owner', 'wrong-source-asset', 'ambiguous', 'conflict', 'query-error'] as const) {
		await t.test(preflight, async sub => {
			const f = harness({ preflight });
			await assert.rejects(() => rebind(sub, f), (error: any) => { assert.match(error.message, /Cannot resolve|BEFORE|identity/i); return true; });
			assert.equal(f.events.some(event => mutationKinds.has(event.kind)), false);
			assert.equal(f.states.has(OLD_ID), true); assert.deepEqual(f.associations.get(DEVICE), OLD_REF);
		});
	}
});

test('native16 preflight context drift refuses mutation after native, scoped, or final association reads', async t => {
	for (const drift of ['native-source', 'scope-query', 'preflight-association'] as const) await t.test(drift, async sub => {
		const f = harness({ drift });
		await assert.rejects(() => rebind(sub, f), (error: any) => { assert.match(`${error.message} ${error.detail}`, /context|project\/document/i); return true; });
		assert.equal(f.events.some(event => mutationKinds.has(event.kind)), false);
		assert.equal(f.states.has(OLD_ID), true);
	});
});

test('native16 drift after device, candidate, or restoration mutation cannot operate on the new page', async t => {
	for (const drift of ['device-modify', 'candidate-create', 'instance-restore'] as const) await t.test(drift, async sub => {
		const f = harness({ drift });
		await assert.rejects(() => rebind(sub, f), (error: any) => {
			const report = facts(error); assert.equal(report.rollback.verified, false);
			assert.match(`${error.message} ${error.detail}`, /context|project\/document/i); return true;
		});
		const driftIndex = f.events.findIndex(event => event.kind === 'context.drift'); assert.ok(driftIndex >= 0);
		assert.equal(f.events.slice(driftIndex + 1).some(event => mutationKinds.has(event.kind)), false);
		assert.equal(f.events.some(event => mutationKinds.has(event.kind) && event.documentUuid === OTHER_PAGE), false);
		if (drift !== 'instance-restore') assert.equal(f.states.has(OLD_ID), true);
		if (drift === 'device-modify') assert.equal(f.states.has(CANDIDATE_ID), false);
	});
});

test('native16 supplierless custom device can reuse a proven personal clone by exact MPN or stable name', async t => {
	for (const custom of ['mpn', 'name'] as const) await t.test(custom, async sub => {
		const f = harness({ custom, cloneFallback: true }), result = await rebind(sub, f);
		assert.equal(result.result.mode, 'cloned-to-personal-library'); assert.equal(result.result.verified, true);
		assert.deepEqual(result.result.device, { uuid: CLONE_DEVICE, libraryUuid: PERSONAL_LIBRARY });
		assert.equal(result.result.component.supplierId, '');
		assert.equal(f.events.some(event => event.kind === 'device.byLcsc'), false);
		assert.equal(f.events.some(event => event.kind === 'device.copy'), false);
		const scoped = f.events.filter(event => event.kind === 'device.search');
		assert.ok(scoped.some(event => event.args[0] === (custom === 'name' ? NAME : MODEL) && event.args[1] === DEVICE_LIBRARY));
		assert.ok(scoped.some(event => event.args[0] === (custom === 'name' ? CLONE_NAME : MODEL) && event.args[1] === PERSONAL_LIBRARY));
	});
});


test('renamed single-unit footprint clone proves pins and preserves NC and PCB identity', async t => {
	const f = harness({ cloneFallback: true, renamedUnit: true });
	const r = await rebind(t, f);
	assert.equal(r.result.component.subPartName, 'Clone.1');
	assert.equal(r.result.component.uniqueId, 'synthetic-stable-link');
	assert.equal((await f.eda.sch_PrimitiveComponent.getAllPinsByPrimitiveId(CANDIDATE_ID))[0].getState_NoConnected(), true);
});

for (const mode of ['ambiguousUnits', 'changedPinGeometry', 'droppedPinNC'] as const) {
	test(`renamed clone refuses ${mode} before original deletion`, async t => {
		const f = harness({ cloneFallback: true, renamedUnit: true, [mode]: true });
		await assert.rejects(() => rebind(t, f));
		assert.equal(f.events.some(e => e.kind === 'component.delete' && e.args[0] === OLD_ID), false);
		assert.equal(f.states.has(OLD_ID), true);
	});
}

 test('clone modify success without association change refuses before candidate creation', async t => {
	const f = harness({ cloneFallback: true, ignoreCloneAssociation: true });
	await assert.rejects(() => rebind(t, f), /candidate-association-readback/);
	assert.equal(f.events.some(e => e.kind === 'component.create'), false);
	assert.equal(f.events.some(e => e.kind === 'device.delete'), false);
	assert.equal(f.states.has(OLD_ID), true);
});

test('failed clone update never deletes a pre-existing personal device', async t => {
	const f = harness({ cloneFallback: true, ignoreCloneAssociation: true, falseCloneAssociation: true });
	await assert.rejects(() => rebind(t, f), /candidate-association-readback/);
	assert.equal(f.events.some(e => e.kind === 'device.delete'), false);
	assert.equal(f.events.some(e => e.kind === 'component.create'), false);
	assert.equal(f.states.has(OLD_ID), true);
});

test('prepared footprint device preserves physical pins without library mutations', async t => {
	const f = harness({ prepared: true });
	const result = await rebind(t, f, true);
	assert.equal(result.result.mode, 'prepared-device');
	assert.equal(result.result.verified, true);
	assert.equal(f.events.some(e => ['device.modify', 'device.copy', 'device.delete'].includes(e.kind)), false);
});
test('prepared device with wrong footprint refuses before mutation', async t => {
	const f = harness();
	await assert.rejects(() => rebind(t, f, true), /exact original model/);
	assert.equal(f.events.some(e => mutationKinds.has(e.kind)), false);
});
for (const mode of ['changedPinGeometry', 'droppedPinNC'] as const) {
	test(`prepared device refuses ${mode} before deleting original`, async t => {
		const f = harness({ prepared: true, [mode]: true });
		await assert.rejects(() => rebind(t, f, true), /candidate-pin-readback/);
		assert.equal(f.states.has(OLD_ID), true);
		assert.equal(f.events.some(e => e.kind === 'component.delete' && e.args[0] === OLD_ID), false);
	});
}

test('prepared rebind normalizes native create rotation before original deletion', async t => {
 const f = harness({prepared:true, renamedUnit:true, invertCreateRotation:true});
 const r = await rebind(t,f,true);
 assert.equal(r.result.verified,true);
 assert.equal(f.states.get(CANDIDATE_ID)?.rotation,90);
 const del=f.events.findIndex(e=>e.kind==='component.delete' && e.args[0]===OLD_ID);
 assert.ok(f.events.slice(0,del).some(e=>e.kind==='component.modify' && e.args[0]===CANDIDATE_ID && (e.args[1] as any).rotation===90));
});

 test('prepared single-unit rebind uses default only after unique mapping proof', async t => {
 const f = harness({ prepared: true, renamedUnit: true });
 const r = await rebind(t, f, true);
 assert.equal(r.result.verified, true);
 assert.equal(f.events.find(e => e.kind === 'component.create')?.args[2], undefined);
 assert.equal(r.result.component.subPartName, 'Clone.1');
 });
 test('prepared ambiguous unit mapping refuses before creating any candidate', async t => {
 const f = harness({ prepared: true, renamedUnit: true, ambiguousUnits: true });
 await assert.rejects(() => rebind(t, f, true));
 assert.equal(f.events.some(e => e.kind === 'component.create'), false);
 assert.equal(f.states.has(OLD_ID), true);
 });

 test('native rebind refuses a busy non-sync host before all mutations', async t => {
 const f = harness({prepared:true, renamedUnit:true});
 f.eda.testRunner = {running:true,currentAction:{name:'InteractivePlace',status:'running'}};
 await assert.rejects(() => rebind(t,f,true), /candidate-create|non-sync/);
 assert.equal(f.events.some(e=>mutationKinds.has(e.kind)),false);
 assert.equal(f.states.has(OLD_ID),true);
 });

 test('physical pin comparison accepts only machine roundoff, not geometric drift', () => {
 const p = {pinNumber:'1',pinName:'IO',x:1010,y:620,rotation:90};
 assert.equal(samePhysicalPinGeometry([p],[{...p,y:619.9999999999999}]),true);
 for (const delta of [1e-8,0.001,1,5]) assert.equal(samePhysicalPinGeometry([p],[{...p,y:620+delta}]),false);
 for (const field of ['pinNumber','pinName']) assert.equal(samePhysicalPinGeometry([p],[{...p,[field]:'different'}]),false);
 assert.equal(samePhysicalPinGeometry([p],[{...p,x:NaN}]),false);
 assert.equal(samePhysicalPinGeometry([p,p],[p,p]),false);
 });

 test('prepared sole-unit mapping accepts verified native instance unit token', async t => {
 const f = harness({prepared:true,renamedUnit:true,nativeUnit:'abcdabcdabcdabcd'});
 const r = await rebind(t,f,true);
 assert.equal(r.result.verified,true);
 assert.equal(r.result.component.subPartName,'abcdabcdabcdabcd');
 });
 test('unexpected non-native candidate unit is refused before original deletion', async t => {
 const f = harness({prepared:true,renamedUnit:true,nativeUnit:'UnknownUnit'});
 await assert.rejects(()=>rebind(t,f,true),/candidate-pin-readback/);
 assert.equal(f.events.some(e=>e.kind==='component.delete' && e.args[0]===OLD_ID),false);
 });


test('prepared initial binding preserves symbol-only original until pin and identity checks pass', async t => {
 const f=harness({prepared:true,initialBinding:true,nativeUnit:'cdcdcdcdcdcdcdcd'});
 const result=await rebind(t,f,true);
 assert.equal(result.result.mode,'prepared-device');
 assert.equal(f.events.some(e=>e.kind.startsWith('device.') && mutationKinds.has(e.kind)),false);
 assert.equal(f.states.has(OLD_ID),false);
 assert.equal(f.states.get(CANDIDATE_ID)?.uniqueId,'synthetic-stable-link');
});
for (const option of ['changedPinGeometry','droppedPinNC','ambiguousUnits'] as const) {
 test(`initial binding retains original when ${option}`,async t=>{
  const f=harness({prepared:true,initialBinding:true,nativeUnit:'cdcdcdcdcdcdcdcd',[option]:true});
  await assert.rejects(rebind(t,f,true));
  assert.equal(f.states.has(OLD_ID),true);
  assert.equal(f.events.some(e=>e.kind==='device.modify'),false);
 });
}
test('unprepared initial binding refuses without library or canvas mutation',async t=>{
 const f=harness({initialBinding:true});
 await assert.rejects(rebind(t,f));
 assert.equal(f.events.some(e=>mutationKinds.has(e.kind)),false);
});

test('initial binding rolls back after a post-deletion restore failure without altering library',async t=>{
 const f=harness({prepared:true,initialBinding:true,nativeUnit:'cdcdcdcdcdcdcdcd',mismatchUniqueId:true});
 await assert.rejects(rebind(t,f,true), (error:any)=>{
  const report=facts(error);
  assert.equal(report.rollback.verified,true,JSON.stringify(report));
  return true;
 });
 assert.equal(f.states.has(CANDIDATE_ID),false);
 assert.equal(f.states.get(RESTORED_ID)?.uniqueId,'synthetic-stable-link');
 assert.equal(f.events.some(e=>e.kind==='device.modify'),false);
});

test('initial binding rejects wrong native candidate footprint before deleting original',async t=>{
 const f=harness({prepared:true,initialBinding:true,nativeUnit:'cdcdcdcdcdcdcdcd',wrongCandidateSource:true});
 await assert.rejects(rebind(t,f,true));
 assert.equal(f.states.has(OLD_ID),true);
 assert.equal(f.events.some(e=>e.kind==='component.delete' && e.args[0]===OLD_ID),false);
});

test('initial binding accepts separately copied sole symbol only after complete physical pin verification', async t=>{
 const f=harness({prepared:true,initialBinding:true,copiedSoleSymbol:true,nativeUnit:'cdcdcdcdcdcdcdcd'});
 const result=await rebind(t,f,true);
 assert.equal(result.result.mode,'prepared-device');
});
test('same sole-unit label on copied symbol never substitutes for pin geometry',async t=>{
 const f=harness({prepared:true,initialBinding:true,copiedSoleSymbol:true,nativeUnit:'cdcdcdcdcdcdcdcd',changedPinGeometry:true});
 await assert.rejects(rebind(t,f,true));
 assert.equal(f.states.has(OLD_ID),true);
});

test('metadata restore reapplies explicit pose and verifies pins after final property write',async t=>{
 const f=harness({prepared:true,initialBinding:true,copiedSoleSymbol:true,nativeUnit:'cdcdcdcdcdcdcdcd',metadataOnlyFlipsPins:true});
 await rebind(t,f,true);
 assert.equal(f.states.get(CANDIDATE_ID)?.flippedPins,false);
});
