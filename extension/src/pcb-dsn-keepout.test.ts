import assert from 'node:assert/strict';
import { test } from 'node:test';
import { injectRegionKeepouts } from './actions';

for (const origin of [0, 300]) test(`DSN keepout uses centerline at origin ${origin}, not rendered stroke bounds`, async t => {
 (globalThis as any).eda = {
  pcb_PrimitiveRegion: {getAll: async () => [{getState_RuleType:()=>[5,6,7],getState_Layer:()=>12,getState_RegionName:()=>null,
   getState_ComplexPolygon:()=>({getSource:()=>[origin,1090,'L',origin+1200,1090,origin+1200,1380,origin,1380,origin,1090]})}]},
  pcb_PrimitivePolyline:{getAll:async()=>[{getState_Polygon:()=>({getSource:()=>[origin,0,'L',origin+1200,0,origin+1200,1100,origin,1100,origin,0]})}]},
  pcb_Primitive:{getPrimitivesBBox:()=>{throw new Error('rendered bounds are not a coordinate reference');}}
 };
 t.after(()=>{delete (globalThis as any).eda;});
 const r=await injectRegionKeepouts('(PCB B (structure (boundary(path signal 0 0 0 1200 0 1200 1100 0 1100 0 0))))');
 assert.equal(r.count,1);
 assert.match(r.text,/polygon signal 0 0 1090 1200 1090 1200 1380 0 1380\)/);
});
test('DSN refuses unavailable region inventory',async t=>{
 (globalThis as any).eda={pcb_PrimitiveRegion:{getAll:async()=>undefined}};
 t.after(()=>{delete (globalThis as any).eda;});
 await assert.rejects(injectRegionKeepouts('(PCB B)'),/inventory unavailable/);
});
