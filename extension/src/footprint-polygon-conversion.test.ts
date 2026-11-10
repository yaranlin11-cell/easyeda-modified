/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { runAction } from './actions';
const polygon = ['POLYGON', [0, 0, 'L', 7.123456789, 0, 5, 3, 0, 0]];
function host(mode = '') {
 let shape: unknown; let saved = false; let deleted = false; let modifies = 0; let creates = 0;
 const primitive = (id = 'p') => {
  let pending: unknown;
  return {getState_PrimitiveId: () => id, toAsync() {return this;}, async reset() {throw new Error('reset must never be used as a read');}, setState_Pad(value:unknown){pending=value;return this;}, async done(){modifies++; await new Promise(resolve=>setTimeout(resolve,5)); shape=pending; if(mode==='commit-error') throw new Error('host commit rejected'); return mode==='wrong-id'?primitive('other'):this;},
   getState_Pad: () => mode === 'drop' || (mode === 'save-drift' && saved) ? ['RECT',1,1,0] : shape,
   getState_PadNumber: () => '1', getState_Layer: () => 1, getState_X: () => mode === 'pose-drift' ? 0.000001 : 0, getState_Y: () => 0, getState_Rotation: () => 0};
 };
 (globalThis as any).eda = {
 dmt_SelectControl: {getCurrentDocumentInfo:async()=>({uuid:'fp',parentLibraryUuid:'lib',documentType:4,tabId:'t'})},
 dmt_EditorControl:{getSplitScreenIdByTabId:async()=> 's',openLibraryDocument:async()=> 't',activateDocument:async()=>true},
 pcb_PrimitivePad:{getAllPrimitiveId:async()=>[],create:async(...args:unknown[])=>{creates++;assert.deepEqual(args[5],['RECT',1,1,0]);return primitive();},modify:async(id:string, p:any)=>{modifies++;shape=p.pad;return mode==='wrong-id'?primitive('other'):primitive(id);},get:async()=>[primitive()],delete:async()=>{deleted=true;return true;}},
 pcb_PrimitivePolyline:{getAllPrimitiveId:async()=>[],delete:async()=>true},pcb_Document:{save:async()=>{saved=true;return true;}}
 };
 return ()=>({saved,deleted,modifies,creates});
}
function build(method: unknown = 'create-then-modify', shape: unknown = polygon) {return runAction('library.footprint.build',{uuid:'fp',libraryUuid:'lib',pads:[{number:'1',layer:1,x:0,y:0,shape,creationMethod:method}]});}
test('polygon conversion awaits commit and reads fresh geometry without reset before and after save',async()=>{const stats=host();try{const r:any=await build();assert.equal(r.result.verified,true);assert.deepEqual(stats(),{saved:true,deleted:false,modifies:1,creates:1});}finally{delete (globalThis as any).eda;}});
test('polygon conversion rejects discarded shape, save loss, pose drift and wrong identity',async()=>{for(const mode of ['drop','save-drift','pose-drift','wrong-id','commit-error']){const stats=host(mode);try{const r:any=await build();assert.equal(r.result.verified,false);assert.equal(r.result.partial,true);assert.equal(stats().deleted,true);}finally{delete (globalThis as any).eda;}}});
test('polygon conversion validates method and shape before writes',async()=>{for(const [method,shape] of [['fallback',polygon],['create-then-modify',['RECT',1,1,0]]]){const stats=host();try{await assert.rejects(()=>build(method,shape),/creationMethod/);assert.equal(stats().creates,0);}finally{delete (globalThis as any).eda;}}});
