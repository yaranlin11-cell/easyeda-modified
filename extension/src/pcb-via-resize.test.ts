import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pcbViaResize } from './pcb-via-resize';
const baseline = { primitiveId: 'v1', net: 'GND', x: 1, y: 2, holeDiameter: 2.8, diameter: 6, locked: false, viaType: 1, designRuleBlindViaName: 'L1-L2' };
const target = { holeDiameter: 0.07 / 0.0254, diameter: 0.15 / 0.0254 };
const getters: Record<string, string> = { primitiveId: 'PrimitiveId', net: 'Net', x: 'X', y: 'Y', holeDiameter: 'HoleDiameter', diameter: 'Diameter', locked: 'PrimitiveLock', viaType: 'ViaType', designRuleBlindViaName: 'DesignRuleBlindViaName' };
function fixture(count = 1, mode = '') {
 const states = Array.from({ length: count }, (_, i) => ({ ...baseline, primitiveId: `v${i + 1}` }));
 let writes = 0;
 const wrap = (s: any) => Object.fromEntries(Object.entries(getters).map(([k, g]) => [`getState_${g}`, () => s[k]]));
 (globalThis as any).EDMT_EditorDocumentType = { PCB: 3 };
 (globalThis as any).eda = { dmt_Project: { getCurrentProjectInfo: async () => ({uuid:'project'}) }, dmt_SelectControl: { getCurrentDocumentInfo: async () => ({uuid:'pcb', documentType:3}) },
 sys_FileManager: { getDocumentSource: async () => JSON.stringify({type:'DOCHEAD'})+'||'+JSON.stringify({docType:'PCB',uuid:'pcb'})+'|\n'+states.map(s => JSON.stringify({type:'VIA',id:s.primitiveId})+'||'+JSON.stringify({netName:s.net,centerX:s.x,centerY:s.y,holeDiameter:s.holeDiameter,viaDiameter:s.diameter,locked:s.locked,viaType:'BLIND',ruleName:s.designRuleBlindViaName})+'|').join('\n') }, pcb_PrimitiveVia: {
 get: async (ids: string | string[]) => Array.isArray(ids) ? states.filter(s => ids.includes(s.primitiveId)).map(wrap) : states.filter(s => s.primitiveId === ids).map(wrap)[0],
 modify: async (id: string, props: any) => {
 writes++; if (mode === 'throw' && writes === 2) throw new Error('host failure');
 assert.deepEqual(Object.keys(props).sort(), ['diameter', 'holeDiameter']);
 const s = states.find(s => s.primitiveId === id)!; Object.assign(s, props);
 if (mode === 'round') s.diameter = 6;
 if (mode === 'metadata') s.designRuleBlindViaName = '';
 return wrap(s);
 }, } };
 return { states, writes: () => writes, payload: () => ({ items: states.map(s => ({ expected: { ...s }, target })) }) };
}
test('via resize preserves metadata and full precision', async () => {
 const f = fixture(); const r = (await pcbViaResize(f.payload())).result!;
 assert.equal(r.verified, true); assert.equal(r.partial, false); assert.equal(r.saved, false);
 assert.equal(f.writes(), 1); assert.deepEqual(f.states[0], { ...baseline, ...target });
});
test('via resize rejects malformed, locked, duplicate and stale batches before writing', async () => {
 const f = fixture(); const p = f.payload();
 for (const bad of [{ items: [] }, { ...p, unexpected: 1 }, { items: [p.items[0], p.items[0]] },
 { items: [{ ...p.items[0], target: { ...target, diameter: NaN } }] },
 { items: [{ ...p.items[0], target: { ...target, startLayer: 1 } }] },
 ...['net', 'primitiveId', 'designRuleBlindViaName'].map(k => ({ items: [{ ...p.items[0], expected: { ...baseline, [k]: 'changed' } }] })),
 { items: [{ ...p.items[0], expected: { ...baseline, locked: true } }] }]) {
 await assert.rejects(() => pcbViaResize(bad), /no writes dispatched/);
 } assert.equal(f.writes(), 0);
});
test('via resize partial host failure stops and retains evidence', async () => {
 const f = fixture(3, 'throw'); const r = (await pcbViaResize(f.payload())).result!;
 assert.equal(r.verified, false); assert.equal(r.partial, true);
 assert.deepEqual(r.attempted, ['v1', 'v2']); assert.deepEqual(r.applied, ['v1']); assert.equal(f.writes(), 2);
 assert.equal(f.states[2].diameter, 6);
});
test('via resize rejects rounded dimensions or lost metadata in readback', async () => {
 for (const mode of ['round', 'metadata']) {
 const f = fixture(1, mode); const r = (await pcbViaResize(f.payload())).result!;
 assert.equal(r.verified, false); assert.equal(r.partial, true); assert.match(String(r.failure), /readback mismatch|missing or invalid via fields/);
 }
});
