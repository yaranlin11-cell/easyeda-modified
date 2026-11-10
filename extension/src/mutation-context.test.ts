/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { beforeEach, test } from 'node:test';
import { runAction } from './actions';
import type { ResponseContext } from './protocol';

beforeEach(t => {
	const globals = globalThis as any;
	const previous = { eda: globals.eda, documentTypes: globals.EDMT_EditorDocumentType };
	globals.EDMT_EditorDocumentType = { HOME: 0, BLANK: 1, SCHEMATIC_PAGE: 2, PCB: 3, SYMBOL_COMPONENT: 4, FOOTPRINT: 5, PANEL: 6 };
	(t as import('node:test').TestContext).after(() => {
		if (previous.eda === undefined) delete globals.eda; else globals.eda = previous.eda;
		if (previous.documentTypes === undefined) delete globals.EDMT_EditorDocumentType; else globals.EDMT_EditorDocumentType = previous.documentTypes;
	});
});

const original: ResponseContext = { projectUuid: 'project', projectName: undefined, documentUuid: 'original-document', documentType: 'pcb', tabId: 'original-tab' };

function host(before: ResponseContext, after: ResponseContext, failProjectBefore = false): { mutatedDocument: () => string | undefined } {
	let active = { ...before }, mutated: string | undefined, projectReads = 0;
	const change = async (): Promise<true> => { mutated = active.documentUuid; active = { ...after }; return true; };
	(globalThis as any).eda = {
		dmt_Project: { getCurrentProjectInfo: async () => {
			if (failProjectBefore && projectReads++ === 0) throw new Error('context read failed');
			return { uuid: active.projectUuid };
		} },
		dmt_SelectControl: { getCurrentDocumentInfo: async () => ({ uuid: active.documentUuid, tabId: active.tabId,
			documentType: active.documentType === 'pcb' ? 3 : active.documentType === 'schematic' ? 2 : undefined }) },
		pcb_PrimitiveVia: { create: async () => { await change(); return { getState_PrimitiveId: () => 'created-via' }; } },
		pcb_Document: { save: change },
	};
	return { mutatedDocument: () => mutated };
}

for (const action of ['pcb.via.create', 'pcb.save']) {
	const payload = action === 'pcb.save' ? {} : { x: 1, y: 2, net: 'GND' };
	test(`${action} certifies stable boundary identity independently of diagnostic context`, async () => {
		host(original, original);
		const got = await runAction(action, payload);
		assert.equal(got.mutationContextAvailable, true);
		assert.deepEqual(got.mutationContext, original);
		assert.deepEqual(got.context, original);
		assert.notEqual(got.mutationContext, got.context);
		assert.equal(got.mutationContextError, undefined);
	});

	for (const field of ['projectUuid', 'documentUuid', 'documentType', 'tabId'] as const) {
		test(`${action} detects ${field} drift without rewriting successful result`, async () => {
			const after = { ...original, [field]: field === 'documentType' ? 'schematic' : 'changed' };
			const fx = host(original, after);
			const got = await runAction(action, payload);
			assert.equal(fx.mutatedDocument(), original.documentUuid);
			assert.equal(got.mutationContextAvailable, false);
			assert.deepEqual(got.mutationContext, original);
			assert.deepEqual(got.context, after, 'post-action activity remains diagnostic evidence');
			assert.match(got.mutationContextError ?? '', new RegExp(field));
			if (action === 'pcb.save') assert.equal(got.result?.saved, true);
			else assert.equal(got.result?.primitiveId, 'created-via');
		});
	}

	for (const side of ['before', 'after'] as const) {
		for (const field of ['projectUuid', 'documentUuid', 'documentType', 'tabId'] as const) {
			test(`${action} marks missing ${side} ${field} unavailable`, async () => {
				const missing = { ...original, [field]: undefined };
				host(side === 'before' ? missing : original, side === 'after' ? missing : original);
				const got = await runAction(action, payload);
				assert.equal(got.mutationContextAvailable, false);
				assert.match(got.mutationContextError ?? '', new RegExp(field));
			});
		}
	}

	test(`${action} accepts tab omitted on both sides`, async () => {
		const withoutTab = { ...original, tabId: undefined };
		host(withoutTab, withoutTab);
		const got = await runAction(action, payload);
		assert.equal(got.mutationContextAvailable, true);
	});

	test(`${action} preserves successful SDK outcome when initial context getter throws`, async () => {
		host(original, original, true);
		const got = await runAction(action, payload);
		assert.equal(got.mutationContextAvailable, false);
		assert.match(got.mutationContextError ?? '', /projectUuid unavailable/);
		if (action === 'pcb.save') assert.equal(got.result?.saved, true);
	});
}
