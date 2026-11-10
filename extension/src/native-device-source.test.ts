import assert from 'node:assert/strict';
import test from 'node:test';
import { projectDeviceSourceInventory, readNativeDeviceSource } from './native-device-source';

const DEVICE_INSTANCE='1'.repeat(16), FP_INSTANCE='2'.repeat(16), DEVICE='3'.repeat(32), LIBRARY='4'.repeat(32), PAGE='5'.repeat(16);
const row=(type: string, payload: Record<string, unknown>) => JSON.stringify({type})+'||'+JSON.stringify(payload)+'|';
const deviceRows=()=>[
	row('DOCHEAD',{docType:'DEVICE',uuid:DEVICE_INSTANCE}),
	row('META',{source:`${DEVICE}|${LIBRARY}`,attributes:{Footprint:FP_INSTANCE}}),
];
const source=()=>[...deviceRows(),row('DOCHEAD',{docType:'SCH_PAGE',uuid:PAGE})].join('\n');

test('same-export DEVICE source binds an exact instance and its footprint association',()=>{
	const inventory=projectDeviceSourceInventory(source(),PAGE);
	assert.deepEqual(readNativeDeviceSource(inventory,DEVICE_INSTANCE),{source:{instanceUuid:DEVICE_INSTANCE,uuid:DEVICE,libraryUuid:LIBRARY,footprintInstanceUuid:FP_INSTANCE,sourceKind:'project-epro2'}});
	assert.deepEqual(readNativeDeviceSource(inventory,'9'.repeat(16)),{});
});

test('DEVICE extraction permits unrelated empty historical payloads and final missing separator',()=>{
	const text=[...deviceRows(),'{"type":"LINE"}|||',row('DOCHEAD',{docType:'SCH_PAGE',uuid:PAGE}).slice(0,-1)].join('\n');
	assert.ok(readNativeDeviceSource(projectDeviceSourceInventory(text,PAGE),DEVICE_INSTANCE).source);
});

test('DEVICE inventory refuses missing or duplicate current page, malformed rows and oversized input',async t=>{
	const cases=[source().replace(PAGE,'9'.repeat(16)),source()+'\n'+row('DOCHEAD',{docType:'SCH_PAGE',uuid:PAGE}),source()+'\ninvalid',source().replace('}|\n','}\n'),source()+'x'.repeat(64*1024*1024)];
	for (const [i,text] of cases.entries()) await t.test(String(i),()=>assert.throws(()=>projectDeviceSourceInventory(text,PAGE)));
});

test('target DEVICE origin cannot use duplicate, foreign, missing or instance-shaped metadata',async t=>{
	const inventory=projectDeviceSourceInventory(source(),PAGE);
	const corruptions=[
		(entries: typeof inventory)=>entries.push({...entries[0]}),
		(entries: typeof inventory)=>{entries[0].documentSource=entries[0].documentSource.replace(DEVICE_INSTANCE,'9'.repeat(16));},
		(entries: typeof inventory)=>{entries[0].documentSource=entries[0].documentSource.replace('"DEVICE"','"FOOTPRINT"');},
		(entries: typeof inventory)=>{entries[0].documentSource=deviceRows()[0];},
		(entries: typeof inventory)=>{entries[0].documentSource+='\n'+deviceRows()[1];},
		(entries: typeof inventory)=>{entries[0].documentSource=entries[0].documentSource.replace(DEVICE+'|',DEVICE_INSTANCE+'|');},
		(entries: typeof inventory)=>{entries[0].documentSource=entries[0].documentSource.replace(`|${LIBRARY}`, '|');},
		(entries: typeof inventory)=>{entries[0].documentSource=entries[0].documentSource.replace(FP_INSTANCE,DEVICE);},
	];
	for (const [i,change] of corruptions.entries()) await t.test(String(i),()=>{
		const entries=structuredClone(inventory); change(entries);
		const proof=readNativeDeviceSource(entries,DEVICE_INSTANCE);
		assert.equal(proof.source,undefined); assert.ok(proof.error);
	});
});

test('symbol-only device requires explicit absent footprint and an exact symbol instance',()=>{
 for(const footprint of ['',null]) {
 const text=[row('DOCHEAD',{docType:'DEVICE',uuid:DEVICE_INSTANCE}),row('META',{source:`${DEVICE}|${LIBRARY}`,attributes:{Footprint:footprint,Symbol:'a'.repeat(16)}}),row('DOCHEAD',{docType:'SCH_PAGE',uuid:PAGE})].join('\n');
 const p=readNativeDeviceSource(projectDeviceSourceInventory(text,PAGE),DEVICE_INSTANCE);
 assert.equal(p.source?.footprintInstanceUuid,'');assert.equal(p.source?.symbolInstanceUuid,'a'.repeat(16));
 }
 const text=[row('DOCHEAD',{docType:'DEVICE',uuid:DEVICE_INSTANCE}),row('META',{source:`${DEVICE}|${LIBRARY}`,attributes:{Symbol:'a'.repeat(16)}}),row('DOCHEAD',{docType:'SCH_PAGE',uuid:PAGE})].join('\n');
 assert.ok(readNativeDeviceSource(projectDeviceSourceInventory(text,PAGE),DEVICE_INSTANCE).error);
});
