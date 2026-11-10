/// <reference types="@jlceda/pro-api-types" />
import { readResponseContext } from './eda-context';
import { ActionError, ErrorCodes } from './protocol';
function refuse(message: string): never { throw new ActionError(ErrorCodes.PRECONDITION_REFUSED, `Gerber export: ${message}`); }
export async function exportPCBArtwork(payload: Record<string, unknown>) {
 if (Object.keys(payload).some(k => !['scope','layers','fileName'].includes(k))) refuse('unsupported fields');
 const scope = payload.scope ?? 'all';
 if (scope !== 'all' && scope !== 'fills-only') refuse('scope must be all or fills-only');
 const name = payload.fileName ?? (scope === 'all' ? 'pcb-gerber.zip' : 'diagnostic-fills-only.zip');
 if (typeof name !== 'string' || !name.trim() || name.length > 128 || /[\\/\x00-\x1f]/.test(name)) refuse('invalid filename');
 const ids = payload.layers;
 if (ids !== undefined && (!Array.isArray(ids) || !ids.length || ids.length>128 || ids.some(x => !Number.isSafeInteger(x) || x<=0) || new Set(ids).size!==ids.length)) refuse('layers must be unique positive layer IDs');
 if (scope === 'fills-only' && !Array.isArray(ids)) refuse('fills-only requires explicit layers; output is diagnostic, not a fabrication package');
 const before = await readResponseContext();
 if (!before.projectUuid || !before.documentUuid || before.documentType !== 'pcb') refuse('exact active PCB identity unavailable');
 if (Array.isArray(ids)) {
  const layers = await eda.pcb_Layer.getAllLayers();
  if (!Array.isArray(layers) || ids.some(id => !layers.some(l => l.id===id))) refuse('requested layer does not exist');
 }
 if (typeof eda.pcb_ManufactureData?.getGerberFile !== 'function') throw new ActionError(ErrorCodes.EDA_API_UNAVAILABLE,'Gerber export API unavailable');
 const file = await eda.pcb_ManufactureData.getGerberFile(name, false, 'mm' as ESYS_Unit.MILLIMETER,
  {integerNumber:4,decimalNumber:6}, scope === 'fills-only' ? {metallicDrillingInformation:false,nonMetallicDrillingInformation:false,drillTable:false,flyingProbeTestingFile:false} : undefined,
  Array.isArray(ids) ? ids.map(id=>({layerId:id,isMirror:false})) : undefined, scope === 'fills-only' ? ['SolidRegion'] : undefined);
 const after = await readResponseContext();
 if (after.projectUuid !== before.projectUuid || after.documentUuid !== before.documentUuid || after.documentType !== 'pcb') throw new ActionError(ErrorCodes.INVALID_STATE,'Active PCB changed during Gerber export; result not accepted');
 if (!file || typeof file.arrayBuffer !== 'function' || !Number.isFinite(file.size) || file.size<4 || file.size>64*1024*1024) throw new ActionError(ErrorCodes.EDA_CALL_FAILED,'Gerber export returned no usable file (4 bytes..64 MiB required)');
 const bytes = new Uint8Array(await file.slice(0,4).arrayBuffer());
 if (bytes[0]!==0x50 || bytes[1]!==0x4b || bytes[2]!==3 || bytes[3]!==4) throw new ActionError(ErrorCodes.EDA_CALL_FAILED,'Gerber output is not a nonempty ZIP archive');
 return {file,name,context:after,result:{scope,layers:ids ?? null,objects:scope==='fills-only'?['SolidRegion']:null,
  unit:'mm',format:{integer:4,decimal:6},size:file.size,diagnosticOnly:scope==='fills-only',manufacturingAcceptance:false}};
}
