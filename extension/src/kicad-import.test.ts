/// <reference types="@jlceda/pro-api-types" />
import {test} from 'node:test';
import assert from 'node:assert/strict';
import JSZip from 'jszip';
import {validateKiCadArchive} from './kicad-import';
import {projectImport} from './project-transfer';
test('KiCad framing accepts quoted parentheses and rejects wrong roots, truncation and extra files',async()=>{
 for (const [name,body,valid] of [
  ['board.kicad_pcb','(kicad_pcb (property "n" "escaped \\\"()"))',true],
  ['board.kicad_pcb','(kicad_pcb',false],
  ['board.kicad_pcb','(kicad_pcb) ()',false],
  ['board.brd','(kicad_pcb (version 1))',false],
  ['../board.kicad_pcb','(kicad_pcb (version 1))',false],
 ] as const) {
  const z=new JSZip();z.file(name,body);const b=await z.generateAsync({type:'uint8array'});
  if(valid) await validateKiCadArchive(b);else await assert.rejects(()=>validateKiCadArchive(b));
 }
 const z=new JSZip();z.file('a.kicad_pcb','(kicad_pcb (version 1))');z.file('b.kicad_pcb','(kicad_pcb (version 1))');
 await assert.rejects(()=>z.generateAsync({type:'uint8array'}).then(validateKiCadArchive));
});
test('KiCad import uses official format and creates a separate project with content unverified',async()=>{
 const z=new JSZip();z.file('board.kicad_pcb','(kicad_pcb (version 20260206))');
 const old=(globalThis as any).eda;let calls=0;
 (globalThis as any).eda={dmt_Project:{getCurrentProjectInfo:async()=>({teamUuid:'team'}),getProjectInfo:async()=>({uuid:'new',teamUuid:'team',friendlyName:'ref'})},sys_FileManager:{importProjectByProjectFile:async(f:File,type:string,p:unknown,s:any)=>{calls++;assert.equal(type,'KiCad');assert.equal(s.operation,'New Project');return {uuid:'new'};}}};
 try {const r=await projectImport({fileName:'board.zip',fileType:'KiCad',base64:await z.generateAsync({type:'base64'}),friendlyName:'ref'});assert.equal(r.result?.contentVerified,false);assert.equal(calls,1);}finally{(globalThis as any).eda=old;}
});
