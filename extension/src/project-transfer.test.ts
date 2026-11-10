/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';
import { validateEdifText } from './project-transfer';

for (const scenario of ['open', 'open-page-delayed', 'open-page-missing', 'open-page-wrong-project', 'open-page-false', 'open-false', 'open-wrong-project', 'open-unconfirmed', 'open-string-confirm', 'open-blank-target', 'open-invalid-page', 'export', 'export-wrong-project', 'export-drift', 'export-empty', 'export-oversized', 'export-size-mismatch', 'export-missing']) {
	test(`typed project transfer: ${scenario}`, async () => {
		const scope = globalThis as unknown as { eda: unknown };
		const old = scope.eda;
		const oldTimer = globalThis.setTimeout;
		let checks = 0, exports = 0, projectOpens = 0, pageReads = 0, pageOpens = 0;
		scope.eda = {
			dmt_Project: {
				openProject: async (id: string) => { projectOpens++; assert.equal(id, 'target'); return scenario !== 'open-false'; },
				getCurrentProjectInfo: async () => ({ uuid: scenario === 'open-wrong-project' || scenario === 'export-wrong-project' || (scenario === 'export-drift' && checks++ > 0) ? 'other' : 'target' }),
			},
			dmt_Schematic: { getAllSchematicPagesInfo: async () => { pageReads++; return scenario === 'open-page-missing' || pageReads === 1 ? [] : [{ uuid: 'page-1' }]; } },
			dmt_EditorControl: { openDocument: async (id: string) => { pageOpens++; assert.equal(id, 'page-1'); return scenario === 'open-page-false' ? undefined : 'tab'; } },
			dmt_SelectControl: { getCurrentDocumentInfo: async () => ({ uuid: 'page-1', parentProjectUuid: scenario === 'open-page-wrong-project' ? 'other' : 'target' }) },
			sys_FileManager: { getProjectFile: async (name: string, unused: unknown, format: string) => {
				exports++; assert.equal(name, 'project.epro2'); assert.equal(unused, undefined); assert.equal(format, 'epro2');
				if (scenario === 'export-missing') return undefined;
				if (scenario === 'export-oversized') return { size: 16777217, arrayBuffer: () => { throw Error('must reject before allocation'); } };
				if (scenario === 'export-size-mismatch') return { size: 1, arrayBuffer: async () => new ArrayBuffer(2) };
				return new Blob([scenario === 'export-empty' ? '' : 'PKfixture']);
			} },
		};
		globalThis.setTimeout = ((f: () => void) => { f(); return 0; }) as unknown as typeof setTimeout;
		try {
			const opening = scenario.startsWith('open');
			const payload: Record<string, unknown> = { projectUuid: scenario === 'open-blank-target' ? ' ' : 'target' };
			if (opening && scenario !== 'open-unconfirmed') payload.allowDiscardUnsaved = scenario === 'open-string-confirm' ? 'true' : true;
			if (scenario.startsWith('open-page')) payload.pageUuid = 'page-1';
			if (scenario === 'open-invalid-page') payload.pageUuid = 42;
			const op = runAction(opening ? 'project.open' : 'project.export', payload);
			if (['open', 'open-page-delayed', 'export'].includes(scenario)) {
				const got = await op;
				assert.equal(got.result?.uuid, 'target');
				if (scenario === 'export') assert.equal(Buffer.from(got.result?.base64 as string, 'base64').toString(), 'PKfixture');
				if (scenario === 'open-page-delayed') { assert.equal(pageOpens, 1); assert.equal(pageReads, 2); assert.equal(got.result?.documentVerified, true); }
			} else await assert.rejects(op);
			if (scenario === 'open-page-missing') assert.equal(pageOpens, 0);
			if (['open-unconfirmed', 'open-string-confirm', 'open-blank-target', 'open-invalid-page'].includes(scenario)) assert.equal(projectOpens, 0);
			if (scenario === 'export-wrong-project') assert.equal(exports, 0);
			assert.ok(projectOpens <= 1);
		} finally { scope.eda = old; globalThis.setTimeout = oldTimer; }
	});
}

for (const scenario of ['import', 'import-edif', 'import-edif-truncated', 'import-edif-dsn', 'import-bad-type', 'import-explicit-team', 'import-no-team', 'import-bad-option', 'import-empty', 'import-none', 'import-unreadable', 'import-blank-name']) {
	test(`typed project import: ${scenario}`, async () => {
		const scope = globalThis as unknown as { eda: unknown };
		const old = scope.eda;
		let imports = 0;
		let seen: { name?: string; size?: number; type?: unknown; props?: unknown; saveTo?: Record<string, unknown> } = {};
		scope.eda = {
			dmt_Project: {
				getCurrentProjectInfo: async () => (scenario === 'import-no-team' ? { uuid: 'active' } : { uuid: 'active', teamUuid: 'team-active' }),
				getProjectInfo: async (id: string) => (scenario === 'import-unreadable' ? undefined : { uuid: id, friendlyName: 'blink', teamUuid: 'team-active' }),
			},
			sys_FileManager: { importProjectByProjectFile: async (file: File, type: unknown, props: unknown, saveTo: Record<string, unknown>) => {
				imports++; seen = { name: file.name, size: file.size, type, props, saveTo };
				return scenario === 'import-none' ? undefined : { uuid: 'new-project' };
			} },
		};
		try {
			const payload: Record<string, unknown> = { fileName: 'blink.epro2', base64: scenario === 'import-empty' ? '' : Buffer.from('PKfixture').toString('base64'),
				friendlyName: scenario === 'import-blank-name' ? ' ' : 'blink' };
			if (scenario === 'import-explicit-team') payload.teamUuid = 'team-x';
			if (scenario === 'import-bad-option') payload.importOption = 'ExtractLibraries';
			if (scenario.startsWith('import-edif')) {
				payload.fileType = 'OrCAD';
				payload.fileName = scenario === 'import-edif-dsn' ? 'design.DSN' : 'design.EDF';
				payload.base64 = Buffer.from(scenario === 'import-edif-truncated' ? '(edif demo' : '(edif demo (edifVersion 2 0 0))').toString('base64');
			}
			if (scenario === 'import-bad-type') payload.fileType = 'guess';
			const op = runAction('project.import', payload);
			if (scenario === 'import-edif') {
				const got = await op;
				assert.equal(seen.type, 'OrCAD');
				assert.equal(seen.name, 'design.EDF');
				assert.equal(seen.saveTo?.operation, 'New Project');
				assert.equal(got.result?.contentVerified, false);
				assert.equal(got.result?.fileType, 'OrCAD');
			} else if (scenario === 'import' || scenario === 'import-explicit-team') {
				const got = await op;
				assert.equal(got.result?.uuid, 'new-project');
				assert.equal(got.result?.contentVerified, false);
				assert.deepEqual(seen, { name: 'blink.epro2', size: 9, type: 'EasyEDA Pro', props: { importOption: 'ImportDocument' },
					saveTo: { operation: 'New Project', newProjectOwnerTeamUuid: scenario === 'import' ? 'team-active' : 'team-x', newProjectFriendlyName: 'blink' } });
			} else await assert.rejects(op);
			if (['import-no-team', 'import-bad-option', 'import-empty', 'import-blank-name', 'import-edif-truncated', 'import-edif-dsn', 'import-bad-type'].includes(scenario)) assert.equal(imports, 0);
			assert.ok(imports <= 1);
		} finally { scope.eda = old; }
	});
}

test('EDIF framing rejects binary/truncation/multiple roots and keeps parentheses in strings', () => {
	const encode = (text: string) => new TextEncoder().encode(text);
	for (const text of ['\ufeff\n(edif sample (edifVersion 2 0 0))', '(edif sample (comment "(text)"))']) validateEdifText(encode(text));
	for (const text of ['(edif sample', '(edif sample)(extra)', '(edif sample))', '<library/>', '(edif sample "oops)', 'DSN', '(edif ' + '('.repeat(1025) + ')'.repeat(1026)]) assert.throws(() => validateEdifText(encode(text)));
	assert.throws(() => validateEdifText(new Uint8Array([0xd0, 0xcf, 0x11, 0xe0])));
});
