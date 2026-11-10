/// <reference types="@jlceda/pro-api-types" />
import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import { runAction } from './actions';
const original = (globalThis as any).eda;
afterEach(() => { if (original === undefined) delete (globalThis as any).eda; else (globalThis as any).eda = original; });
function host(width: (() => unknown) | undefined, visibility: Record<string, unknown> = {}): void {
 const common: any = {
  getState_PrimitiveId: () => 'text', getState_Layer: () => 3, getState_X: () => 10, getState_Y: () => 20,
  getState_LineWidth: width, getState_FontSize: () => 32, getState_FontFamily: () => 'default',
  getState_Value: () => 'U1', getState_Text: () => 'BAT+', ...visibility,
 };
 (globalThis as any).eda = {
  pcb_PrimitiveComponent: { getAll: async () => [] }, pcb_Primitive: { getPrimitivesBBox: async () => null },
  pcb_PrimitiveAttribute: { getAll: async () => [common] }, pcb_PrimitiveString: { getAll: async () => [common] },
 };
}
for (const value of [0, 6]) {
 test(`silk captures measured ${value} mil without style defaults`, async () => {
  host(() => value); const result: any = (await runAction('pcb.silk.list', {})).result;
  for (const text of result.texts) {
   assert.equal(text.lineWidth, value); assert.equal(text.lineWidthAvailability, 'available');
   assert.equal(text.lineWidthSource, 'getState_LineWidth'); assert.equal(text.fontSize, 32);
  }
 });
}
for (const [label, getter] of [
 ['missing', undefined], ['undefined', () => undefined], ['null', () => null], ['NaN', () => NaN],
 ['negative', () => -1], ['string', () => '6'], ['throw', () => { throw new Error('unavailable width'); }],
] as const) {
 test(`silk ${label} line width remains unknown while text is retained`, async () => {
  host(getter); const result: any = (await runAction('pcb.silk.list', {})).result;
  assert.equal(result.texts.length, 2);
  for (const text of result.texts) { assert.equal(text.lineWidth, undefined); assert.equal(text.lineWidthAvailability, 'unknown'); assert.ok(text.lineWidthError); }
 });
}

for (const visible of [true, false]) {
 test(`silk preserves explicit attribute value visibility ${visible}`, async () => {
  host(() => 6, { getState_ValueVisible: () => visible, getState_KeyVisible: () => !visible });
  const result: any = (await runAction('pcb.silk.list', {})).result;
  const [attribute, string] = result.texts;
  assert.equal(attribute.visible, visible); assert.equal(attribute.valueVisible, visible);
  assert.equal(attribute.visibilityAvailability, 'available'); assert.equal(attribute.visibilitySource, 'getState_ValueVisible');
  assert.equal(attribute.keyVisible, !visible); assert.equal(attribute.keyVisibilityAvailability, 'available');
  assert.equal(string.visible, true); assert.equal(string.visibilitySource, 'primitive-string');
 });
}
for (const [label, getter] of [
 ['missing', undefined], ['null', () => null], ['number', () => 0],
 ['throw', () => { throw new Error('visibility unavailable'); }],
] as const) {
 test(`silk ${label} attribute visibility is unknown independently of line width and strings`, async () => {
  host(() => 6, { getState_ValueVisible: getter, getState_KeyVisible: getter });
  const result: any = (await runAction('pcb.silk.list', {})).result;
  const [attribute, string] = result.texts;
  assert.equal(attribute.visible, undefined); assert.equal(attribute.valueVisible, undefined);
  assert.equal(attribute.visibilityAvailability, 'unknown'); assert.ok(attribute.visibilityError);
  assert.equal(attribute.keyVisible, undefined); assert.equal(attribute.keyVisibilityAvailability, 'unknown');
  assert.equal(attribute.lineWidth, 6); assert.equal(string.visible, true); assert.equal(string.visibilityAvailability, 'available');
 });
}
