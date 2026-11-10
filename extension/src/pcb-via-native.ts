/// <reference types="@jlceda/pro-api-types" />
import { readResponseContext } from './eda-context';
import { ActionError, ErrorCodes } from './protocol';
export interface NativeVia {
 primitiveId: string; net: string; x: number; y: number; holeDiameter: number; diameter: number;
 locked: boolean; viaType: number; designRuleBlindViaName: string;
}
function fail(message: string): never { throw new ActionError(ErrorCodes.INVALID_STATE, `Native via readback: ${message}`); }
export function parseNativeVias(source: unknown, documentUuid: string): NativeVia[] {
 if (typeof source !== 'string' || !source.trim() || source.length > 64 * 1024 * 1024) fail('source unavailable or larger than 64 MiB');
 const lines = source.trimEnd().split(/\r?\n/), rows: NativeVia[] = [], ids = new Set<string>();
 let heads = 0;
 for (const [index, line] of lines.entries()) {
  const split = line.indexOf('||'); if (split < 1) fail('malformed record framing');
  let header: any, value: any;
  try { header = JSON.parse(line.slice(0, split)); value = JSON.parse(line.slice(split + 2).replace(/\|$/, '')); }
  catch { fail('malformed record JSON'); }
  if (!header || typeof header.type !== 'string' || !value || typeof value !== 'object' || Array.isArray(value)) fail('unsupported record');
  if (header.type === 'DOCHEAD') {
   heads++; if (index !== 0 || heads !== 1 || value.docType !== 'PCB' || value.uuid !== documentUuid) fail('wrong PCB identity');
  }
  if (header.type !== 'VIA') continue;
  if (typeof header.id !== 'string' || !header.id || ids.has(header.id)) fail('duplicate or missing via ID');
  ids.add(header.id);
  if (!['centerX','centerY','holeDiameter','viaDiameter'].every(k => typeof value[k] === 'number' && Number.isFinite(value[k]))
   || value.holeDiameter <= 0 || value.viaDiameter <= value.holeDiameter || typeof value.locked !== 'boolean'
   || typeof value.netName !== 'string' || !['NORMAL','BLIND','SUTURE'].includes(value.viaType)
   || (value.ruleName != null && typeof value.ruleName !== 'string') || (value.viaType === 'BLIND' && !value.ruleName)) fail('missing or invalid via fields');
  rows.push({ primitiveId: header.id, net: value.netName, x: value.centerX, y: value.centerY,
   holeDiameter: value.holeDiameter, diameter: value.viaDiameter, locked: value.locked,
   viaType: ['NORMAL','BLIND','SUTURE'].indexOf(value.viaType), designRuleBlindViaName: value.ruleName ?? '' });
 }
 if (heads !== 1) fail('PCB DOCHEAD missing');
 return rows;
}
export async function readNativeVias(): Promise<NativeVia[]> {
 const before = await readResponseContext();
 if (before.documentType !== 'pcb' || !before.projectUuid || !before.documentUuid) fail('exact active PCB context unavailable');
 if (typeof eda.sys_FileManager?.getDocumentSource !== 'function') fail('official source getter unavailable');
 const source = await eda.sys_FileManager.getDocumentSource();
 const after = await readResponseContext();
 if (after.projectUuid !== before.projectUuid || after.documentUuid !== before.documentUuid || after.documentType !== 'pcb') fail('active document changed');
 return parseNativeVias(source, before.documentUuid);
}
