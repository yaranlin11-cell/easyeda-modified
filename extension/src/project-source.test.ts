/// <reference types="@jlceda/pro-api-types" />

import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';

test('project source export preserves official archive bytes and checks current project around archive getter', async () => {
	const bytes = Uint8Array.from([0x50, 0x4b, 0x03, 0x04, 0, 255, 42]);
	const calls: unknown[][] = [];
	(globalThis as any).eda = {
		dmt_Project: { getCurrentProjectInfo: async () => { calls.push(['current']); return { uuid: 'project-id' }; } },
		sys_FileManager: { getProjectFile: async (...args: unknown[]) => {
			calls.push(['export', ...args]);
			return new Blob([bytes]);
		} },
	};
	try {
		const got: any = await runAction('project.export_source', { uuid: 'project-id' });
		assert.deepEqual(calls.filter(call => call[0] === 'export'), [['export', 'easyeda-agent-project.epro2', undefined, 'epro2']]);
		const exportIndex = calls.findIndex(call => call[0] === 'export');
		assert.equal(calls[exportIndex - 1]?.[0], 'current', 'current project must be read before the archive getter');
		assert.equal(calls[exportIndex + 1]?.[0], 'current', 'current project must be rechecked after the archive getter');
		assert.equal(got.result.size, bytes.length);
		assert.equal(got.result.artifactId, got.artifacts[0].id);
		assert.deepEqual(Buffer.from(got.artifacts[0].inlineBase64, 'base64'), Buffer.from(bytes));
	}
	finally { delete (globalThis as any).eda; }
});

test('project source export rejects identity mismatch before or after getter', async () => {
	for (const phase of ['before', 'after'] as const) {
		let currentProject = phase === 'before' ? 'wrong' : 'project-id';
		let exports = 0;
		(globalThis as any).eda = {
			dmt_Project: { getCurrentProjectInfo: async () => ({ uuid: currentProject }) },
			sys_FileManager: { getProjectFile: async () => {
				exports++;
				if (phase === 'after') currentProject = 'wrong';
				return new Blob(['archive']);
			} },
		};
		try {
			await assert.rejects(
				() => runAction('project.export_source', { uuid: 'project-id' }),
				(err: any) => err.code === 'INVALID_STATE' && /project/i.test(err.message),
			);
			assert.equal(exports, phase === 'before' ? 0 : 1, 'identity drift must reject at the actual getter boundary');
		}
		finally { delete (globalThis as any).eda; }
	}
});

test('project source export rejects missing, empty and oversized files before encoding', async () => {
	for (const file of [undefined, new Blob([]), { size: (8 << 20) + 1 }]) {
		(globalThis as any).eda = {
			dmt_Project: { getCurrentProjectInfo: async () => ({ uuid: 'p' }) },
			sys_FileManager: { getProjectFile: async () => file },
		};
		try {
			await assert.rejects(
				() => runAction('project.export_source', { uuid: 'p' }),
				(err: any) => err.code === 'INVALID_STATE' && /transfer limit/.test(err.message),
			);
		}
		finally { delete (globalThis as any).eda; }
	}
});

test('project source export requires expected UUID and official getter', async () => {
	(globalThis as any).eda = { sys_FileManager: {} };
	try {
		await assert.rejects(
			() => runAction('project.export_source', {}),
			(err: any) => err.code === 'MISSING_PAYLOAD_FIELD',
		);
		await assert.rejects(
			() => runAction('project.export_source', { uuid: 'p' }),
			(err: any) => err.code === 'EDA_API_UNAVAILABLE',
		);
	}
	finally { delete (globalThis as any).eda; }
});
