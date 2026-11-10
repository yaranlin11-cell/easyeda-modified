import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseNativeVias } from './pcb-via-native';
const head = '{"type":"DOCHEAD"}||{"docType":"PCB","uuid":"pcb"}|\n';
const record = '{"type":"VIA","id":"ie1"}||{"netName":"N","centerX":1.23456,"centerY":-2,"holeDiameter":2.75591,"viaDiameter":5.90551,"locked":false,"viaType":"BLIND","ruleName":"L1-L2"}|';
test('native via read retains microvia precision instead of SDK rounding', () => {
 const [via] = parseNativeVias(head+record,'pcb');
 assert.equal(via.holeDiameter,2.75591); assert.equal(via.diameter,5.90551); assert.equal(via.x,1.23456); assert.equal(via.viaType,1);
});
test('native via read refuses wrong identity, duplicate IDs, malformed or missing source', () => {
 for (const text of ['',record,head+record+'\n'+record,head+record.replace('BLIND','unknown'),head+record.replace('5.90551','null'),head+record+'\nbad',head.replace('"pcb"','"other"')+record]) {
  assert.throws(()=>parseNativeVias(text,'pcb'),/Native via readback/);
 }
 assert.deepEqual(parseNativeVias(head,'pcb'),[]);
});

test('native NORMAL via maps to SDK through type, without requiring a blind rule', () => {
 const [v] = parseNativeVias(head+record.replace('BLIND','NORMAL').replace(',"ruleName":"L1-L2"',''),'pcb');
 assert.equal(v.viaType,0); assert.equal(v.designRuleBlindViaName,'');
});
