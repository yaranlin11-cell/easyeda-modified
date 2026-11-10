/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { beforeEach, test } from 'node:test';
import { ActionQueue } from './action-queue';
import { runAction } from './actions';
import { ActionError, ErrorCodes } from './protocol';

beforeEach(t => {
	const globals = globalThis as any;
	const previous = { eda: globals.eda, documentTypes: globals.EDMT_EditorDocumentType };
	globals.EDMT_EditorDocumentType = { HOME: 0, BLANK: 1, SCHEMATIC_PAGE: 2, PCB: 3, SYMBOL_COMPONENT: 4, FOOTPRINT: 5, PANEL: 6 };
	(t as import('node:test').TestContext).after(() => {
		if (previous.eda === undefined) delete globals.eda; else globals.eda = previous.eda;
		if (previous.documentTypes === undefined) delete globals.EDMT_EditorDocumentType; else globals.EDMT_EditorDocumentType = previous.documentTypes;
	});
});

for (const documentType of ['schematic', 'pcb'] as const) {
	const action = `${documentType}.save`;
	const expectedContext = { projectUuid: 'project', documentUuid: 'document', documentType, tabId: 'tab' };

	function host(save: () => Promise<unknown>, overrides: { projectUuid?: string | null; documentUuid?: string | null; documentType?: number | null; tabId?: string | null } = {}): void {
		const globals = globalThis as any;
		const state = {
			projectUuid: 'project', documentUuid: 'document',
			documentType: documentType === 'schematic' ? 2 : 3, tabId: 'tab', ...overrides,
		};
		globals.eda = {
			dmt_Project: { getCurrentProjectInfo: async () => state.projectUuid === null ? undefined : { uuid: state.projectUuid } },
			dmt_SelectControl: { getCurrentDocumentInfo: async () => state.documentUuid === null ? undefined : { uuid: state.documentUuid, documentType: state.documentType, tabId: state.tabId } },
			[documentType === 'schematic' ? 'sch_Document' : 'pcb_Document']: { save },
		};
	}

	for (const saved of [true, false]) {
		test(`${action} calls SDK save after exact context validation and retains saved=${saved}`, async () => {
			let calls = 0;
			host(async () => { calls++; return saved; });
			const got: any = await runAction(action, { expectedContext });
			assert.equal(calls, 1);
			assert.equal(got.result.saved, saved);
		});
	}

	for (const field of ['projectUuid', 'documentUuid', 'documentType', 'tabId'] as const) {
		test(`${action} refuses mismatched ${field} before calling SDK save`, async () => {
			let calls = 0;
			host(async () => { calls++; return true; }, { [field]: field === 'documentType' ? 5 : 'changed' });
			await assert.rejects(runAction(action, { expectedContext }), (err: unknown) => {
				assert.ok(err instanceof ActionError);
				assert.equal(err.code, ErrorCodes.PRECONDITION_REFUSED);
				assert.ok(JSON.parse(err.detail!).mismatchedFields.includes(field));
				return true;
			});
			assert.equal(calls, 0);
		});

		test(`${action} refuses unreadable ${field} before calling SDK save`, async () => {
			let calls = 0;
			host(async () => { calls++; return true; }, { [field]: undefined });
			await assert.rejects(runAction(action, { expectedContext }), (err: unknown) => {
				assert.ok(err instanceof ActionError);
				assert.equal(err.code, ErrorCodes.PRECONDITION_REFUSED);
				assert.ok(JSON.parse(err.detail!).unavailableFields.includes(field));
				return true;
			});
			assert.equal(calls, 0);
		});
	}

	test(`${action} permits an omitted tabId but validates all three required identity fields`, async () => {
		let calls = 0;
		host(async () => { calls++; return true; }, { tabId: undefined });
		const { tabId: _ignored, ...withoutTab } = expectedContext;
		const got: any = await runAction(action, { expectedContext: withoutTab });
		assert.equal(calls, 1);
		assert.equal(got.result.saved, true);
	});

	test(`${action} refuses failed context SDK reads before calling save`, async () => {
		let calls = 0;
		host(async () => { calls++; return true; });
		(globalThis as any).eda.dmt_Project.getCurrentProjectInfo = async () => { throw new Error('context unavailable'); };
		await assert.rejects(runAction(action, { expectedContext }), { code: ErrorCodes.PRECONDITION_REFUSED });
		assert.equal(calls, 0);
	});

	test(`${action} rejects malformed or incomplete expectedContext without saving`, async () => {
		let calls = 0;
		host(async () => { calls++; return true; });
		for (const value of [null, [], 'document', {}, { ...expectedContext, documentUuid: '' }, { ...expectedContext, tabId: 1 }]) {
			await assert.rejects(runAction(action, { expectedContext: value }), { code: ErrorCodes.MISSING_PAYLOAD_FIELD });
		}
		await assert.rejects(runAction(action, { expectedContext: { ...expectedContext, documentType: documentType === 'pcb' ? 'schematic' : 'pcb' } }), { code: ErrorCodes.PRECONDITION_REFUSED });
		assert.equal(calls, 0);
	});

	test(`${action} keeps the existing unguarded request compatible`, async () => {
		let calls = 0;
		host(async () => { calls++; return true; }, { projectUuid: null, documentUuid: null });
		const got: any = await runAction(action, {});
		assert.equal(calls, 1);
		assert.equal(got.result.saved, true);
	});
}

test('guarded save checks the active document when it reaches FIFO, after preceding actions settle', async () => {
	let documentUuid = 'document';
	let saveCalls = 0;
	let contextReads = 0;
	(globalThis as any).eda = {
		dmt_Project: { getCurrentProjectInfo: async () => { contextReads++; return { uuid: 'project' }; } },
		dmt_SelectControl: { getCurrentDocumentInfo: async () => ({ uuid: documentUuid, documentType: 3 }) },
		pcb_Document: { save: async () => { saveCalls++; return true; } },
	};
	const queue = new ActionQueue({ graceMs: 0 });
	let release!: () => void;
	const held = new Promise<void>(resolve => { release = resolve; });
	const first = queue.submit({ id: 'earlier-action', run: () => held });
	const save = queue.submit({
		id: 'guarded-save',
		run: () => runAction('pcb.save', { expectedContext: { projectUuid: 'project', documentUuid: 'document', documentType: 'pcb' } }),
	});
	assert.equal(contextReads, 0, 'context check must wait for handler execution');
	documentUuid = 'different-document';
	release();
	assert.equal((await first).status, 'ok');
	const outcome = await save;
	assert.equal(outcome.status, 'error');
	if (outcome.status === 'error') assert.equal((outcome.error as ActionError).code, ErrorCodes.PRECONDITION_REFUSED);
	assert.equal(saveCalls, 0);
	assert.equal(contextReads, 2, 'boundary sample and guarded save both run only at FIFO execution');
});
