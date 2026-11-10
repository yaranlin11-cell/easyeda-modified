import assert from 'node:assert/strict';
import test from 'node:test';
import {proveNativeSymbolOnlyDevice, NativePlacedDeviceSource} from './placed-device-identity';
const lib='a'.repeat(32), uuid='b'.repeat(32), inst='c'.repeat(16), sym='d'.repeat(16);
const origin:NativePlacedDeviceSource={instanceUuid:inst,uuid,libraryUuid:lib,footprintInstanceUuid:'',symbolInstanceUuid:sym,sourceKind:'project-epro2'};
const snapshot=()=>({device:{uuid:inst,libraryUuid:lib},component:{uuid:inst},symbol:{uuid:sym},footprint:{uuid:''},manufacturer:'vendor',manufacturerId:'exact-model',supplierId:''});
const detail=()=>({uuid,libraryUuid:lib,association:{footprint:{uuid:'',libraryUuid:lib},symbol:{uuid:'e'.repeat(32),libraryUuid:lib}},property:{manufacturer:'vendor',manufacturerId:'exact-model',supplierId:''}});
test('symbol-only exact origin proves identity without inventing a footprint',async()=>{
 let guards=0,gets=0;
 const r=await proveNativeSymbolOnlyDevice(snapshot(),origin,{assertContext:async()=>{guards++;},get:async()=>{gets++;return detail();},search:async()=>{throw Error('search forbidden');},byLcsc:async()=>{throw Error('search forbidden');}});
 assert.equal(r.device?.uuid,uuid);assert.equal(r.footprintSource,undefined);assert.equal(guards,2);assert.equal(gets,1);
});
test('symbol-only proof rejects conflicts, unexpected footprint, wrong symbol owner, and context drift',async()=>{
 for(const mode of ['mpn','vendor','uuid','footprint','owner','context']) {
 const s=snapshot();const d:any=detail();
 if(mode==='mpn')d.property.manufacturerId='other';if(mode==='vendor')d.property.manufacturer='other';if(mode==='uuid')d.uuid='f'.repeat(32);
 if(mode==='footprint')d.association.footprint={uuid:'f'.repeat(32),libraryUuid:lib};if(mode==='owner')s.symbol.uuid='0'.repeat(16);
 const r=await proveNativeSymbolOnlyDevice(s,origin,{assertContext:async()=>{if(mode==='context')throw Error('drift');},get:async()=>d,search:async()=>[],byLcsc:async()=>[]});
 assert.equal(r.device,undefined,mode);assert.ok(r.reason,mode);
 }
});
