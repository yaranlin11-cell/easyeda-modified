import test from 'node:test';
import assert from 'node:assert/strict';
import { exportPCBArtwork } from './pcb-gerber';
function fixture(mode='ok') {
 let doc='d'; const calls:unknown[][]=[];
 (globalThis as any).EDMT_EditorDocumentType={PCB:3};
 (globalThis as any).eda={dmt_Project:{getCurrentProjectInfo:async()=>({uuid:'p'})},dmt_SelectControl:{getCurrentDocumentInfo:async()=>({uuid:doc,documentType:3})},pcb_Layer:{getAllLayers:async()=>[{id:1},{id:15}]},pcb_ManufactureData:{getGerberFile:async(...args:unknown[])=>{calls.push(args);if(mode==='drift')doc='other';if(mode==='none')return undefined;return new File([mode==='empty'?'':mode==='html'?'<html>':new Uint8Array([80,75,3,4,0])],'test.zip');}}};
 return calls;
}
test('diagnostic export explicitly filters copper fills and preserves requested precision',async()=>{
 const calls=fixture();const r=await exportPCBArtwork({scope:'fills-only',layers:[1,15]});
 assert.equal(r.result.diagnosticOnly,true);assert.equal(r.result.manufacturingAcceptance,false);
 assert.equal(calls[0][2],'mm');assert.deepEqual(calls[0][3],{integerNumber:4,decimalNumber:6});assert.deepEqual(calls[0][6],['SolidRegion']);
 assert.deepEqual(calls[0][5],[{layerId:1,isMirror:false},{layerId:15,isMirror:false}]);
});
test('invalid payloads refuse before export',async()=>{
 for(const payload of [{scope:'guess'},{scope:'fills-only'},{layers:[1,1]},{layers:[99]},{layers:[NaN]},{fileName:'../x'},{other:true}]) { const calls=fixture();await assert.rejects(exportPCBArtwork(payload));assert.equal(calls.length,0); }
});
test('identity drift and invalid archive reject',async()=>{for(const mode of ['drift','none','empty','html']){fixture(mode);await assert.rejects(exportPCBArtwork({}));}});
test('full export does not implicitly filter object types or auxiliary output',async()=>{const calls=fixture();const r=await exportPCBArtwork({});assert.equal(r.result.diagnosticOnly,false);assert.equal(calls[0][4],undefined);assert.equal(calls[0][5],undefined);assert.equal(calls[0][6],undefined);});
