/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';

test('PCB DRC never turns unavailable or boolean host results into a verdict', async () => {
 for (const value of [undefined, null, false, true, {}, '']) {
  let bindingReads = 0;
  (globalThis as any).eda = { pcb_Drc: { check: async () => value }, dmt_Board: { getCurrentBoardInfo: async () => { bindingReads++; return {}; } } };
  try {
   await assert.rejects(() => runAction('pcb.drc.check', {}), (e: any) => e.code === 'PRECONDITION_REFUSED' && /no valid DRC verdict/.test(e.message));
   assert.equal(bindingReads, 0);
  } finally { delete (globalThis as any).eda; }
 }
});

test('PCB DRC retains valid empty and failing arrays and survives missing binding diagnostics', async () => {
 for (const violations of [[], [{ name: 'Netlist Error', list: [{ errorType: 'mismatch' }] }]]) {
  (globalThis as any).eda = { pcb_Drc: { check: async () => violations }, dmt_Board: { getCurrentBoardInfo: async () => { throw new Error('diagnostic unavailable'); } } };
  try { const res: any = await runAction('pcb.drc.check', {}); assert.equal(res.result.passed, violations.length === 0); assert.deepEqual(res.result.violations, violations); }
  finally { delete (globalThis as any).eda; }
 }
});
