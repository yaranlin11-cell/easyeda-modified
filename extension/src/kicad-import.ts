import JSZip from 'jszip';
import { ActionError, ErrorCodes } from './protocol';
const LIMIT = 128 * 1024 * 1024;
function fail(): never { throw new ActionError(ErrorCodes.PRECONDITION_REFUSED, 'Expected one root .kicad_pcb in ZIP, bounded to 128 MiB; no project imported.'); }
export async function validateKiCadArchive(bytes: Uint8Array): Promise<void> {
 const z = await JSZip.loadAsync(bytes);
 const entries = Object.values(z.files);
 if (entries.length !== 1) fail();
 const e = entries[0];
 if (e.dir || /[/\\]/.test(e.name) || !/\.kicad_pcb$/i.test(e.name) || (e.unsafeOriginalName !== undefined && e.unsafeOriginalName !== e.name)) fail();
 // Stream with a hard expanded-size limit; do not inflate an unbounded buffer.
 const chunks: Uint8Array[] = []; let size = 0;
 await new Promise<void>((resolve,reject) => {
  // JSZip 3.10 implements this documented streaming method on ZipObject,
  // but its bundled declarations only expose the helper on archive generation.
  const stream = (e as unknown as { internalStream(type: 'uint8array'): JSZip.JSZipStreamHelper<Uint8Array> }).internalStream('uint8array');
  stream.on('data',(chunk: Uint8Array) => {
   size += chunk.length;
   if (size > LIMIT) { stream.pause(); reject(new Error('KiCad board exceeds 128 MiB')); return; }
   chunks.push(chunk);
  }).on('error',reject).on('end',resolve).resume();
 });
 const data = new Uint8Array(size); let offset = 0;
 for (const c of chunks) { data.set(c,offset); offset += c.length; }
 const s = new TextDecoder('utf-8',{fatal:true}).decode(data).trim();
 if (!/^\(kicad_pcb\s/.test(s)) fail();
 let depth = 0,quoted = false,escaped = false;
 for (let i=0;i<s.length;i++) {
  const c=s[i];
  if (quoted) { if(escaped) escaped=false; else if(c==='\\') escaped=true; else if(c==='"') quoted=false; continue; }
  if(c==='"') {quoted=true;continue;}
  if(c==='(') depth++; if(c===')') depth--;
  if(depth<0 || depth>1024 || (depth===0 && i!==s.length-1)) fail();
 }
 if(quoted || depth!==0) fail();
}
