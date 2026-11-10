/// <reference types="@jlceda/pro-api-types" />
import { documentSourceGet } from './document-source';
import { readResponseContext } from './eda-context';
import { ActionError, ErrorCodes } from './protocol';
import { describeThrown } from './util';

type JsonObject = Record<string, unknown>;
export interface PourOrderTarget { readonly projectUuid: string; readonly documentUuid: string; readonly documentType: 'pcb' }
export interface NativePourOrder {
	readonly primitiveId: string;
	readonly layer: number;
	readonly order: number;
	/** Complete native payload, including fields unknown to this adapter. */
	readonly payload: Readonly<JsonObject>;
}
export interface NativePourInventory { readonly source: string; readonly pours: readonly NativePourOrder[] }
export interface PourOrderPlan {
	readonly target: PourOrderTarget;
	readonly layer: number;
	readonly requested: number;
	readonly baseline: NativePourInventory;
}
export interface PourOrderEvidence {
	primitiveId: string;
	requested: number;
	orderWriteAttempted: boolean;
	baselineSource: string;
	beforeOrderSource?: string;
	afterOrderSource?: string;
	afterRebuildSource?: string;
	sdkPriorityBefore?: unknown;
	sdkPriorityReadError?: string;
	beforeNativeOrder?: number;
	afterNativeOrder?: number;
}
export interface PourOrderVerification extends PourOrderEvidence {
	verified: true;
	nativeOrder: number;
	target: PourOrderTarget;
	finalOrders: Array<{ primitiveId: string; layer: number; order: number }>;
}

/** Always use a post-create failure code: even a local preflight failure here
 * leaves the separately created pour in the document. The caller can retain
 * evidence in a partial result; an uncaught error still names that exact ID. */
export class PourOrderError extends ActionError {
	constructor(message: string, public readonly evidence: PourOrderEvidence) {
		super(ErrorCodes.EDA_CALL_FAILED, `Pour ${evidence.primitiveId}: ${message}`, JSON.stringify(evidence));
		this.name = 'PourOrderError';
	}
}

class NativePourSourceError extends ActionError {
	constructor(message: string, public readonly source: string) {
		super(ErrorCodes.PRECONDITION_REFUSED, message, JSON.stringify({ source }));
	}
}

function refuse(message: string): never {
	throw new ActionError(ErrorCodes.PRECONDITION_REFUSED, message);
}
function object(value: unknown): value is JsonObject {
	return value !== null && typeof value === 'object' && !Array.isArray(value);
}
function freeze<T>(value: T): T {
	if (value !== null && typeof value === 'object') {
		for (const child of Object.values(value)) freeze(child);
		Object.freeze(value);
	}
	return value;
}
function canonical(value: unknown): string {
	if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
	if (object(value)) return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`;
	return JSON.stringify(value);
}
function withoutOrder(payload: Readonly<JsonObject>): string {
	const value = { ...payload };
	delete value.order;
	return canonical(value);
}
function parseObject(text: string, label: string): JsonObject {
	let value: unknown;
	try { value = JSON.parse(text); }
	catch { refuse(`Malformed ${label}; native pour inventory is unavailable.`); }
	if (!object(value)) refuse(`${label} must be an object; native pour inventory is unavailable.`);
	return value;
}

/** Narrow read-only projection of the official TPcbPour schema shipped in
 * pro-api/0.3.38.25cc6d44 assets/format-types.schema-NNUKW5PX.json. The source
 * is a snapshot, not an event-log replay: ambiguous duplicate POUR IDs or
 * missing payloads are refused rather than interpreted as deletions/emptiness. */
export function parseNativePourOrderSource(source: unknown, target: PourOrderTarget): NativePourInventory {
	if (typeof source !== 'string' || !source.trim()) refuse('Native PCB source is empty or unavailable.');
	const lines = source.split(/\r?\n/);
	if (lines[lines.length - 1] === '') lines.pop();
	const pours: NativePourOrder[] = [];
	let docheads = 0;
	const ids = new Set<string>();
	for (let index = 0; index < lines.length; index++) {
		const line = lines[index];
		if (!line) refuse('Blank native source record is unsupported.');
		const separator = line.indexOf('||');
		if (separator < 1) refuse('Native source record framing is unavailable.');
		const header = parseObject(line.slice(0, separator), 'native record header');
		if (typeof header.type !== 'string' || !header.type) refuse('Native record type is unavailable.');
		let payloadText = line.slice(separator + 2);
		if (payloadText.endsWith('|')) payloadText = payloadText.slice(0, -1);
		// Parse every payload so a truncated/malformed row cannot conceal a POUR.
		const payload = parseObject(payloadText, 'native record payload');
		if (header.type === 'DOCHEAD') {
			docheads++;
			if (index !== 0 || docheads !== 1 || payload.docType !== 'PCB' || payload.uuid !== target.documentUuid) {
				refuse('Native DOCHEAD does not identify the exact PCB target.');
			}
		}
		if (header.type !== 'POUR') continue;
		if (typeof header.id !== 'string' || !/^[0-9a-f]{16}$/.test(header.id)) refuse('Native POUR ID is missing or unsupported.');
		if (!Number.isSafeInteger(header.ticket) || Number(header.ticket) < 0) refuse('Native POUR ticket is unavailable.');
		if (ids.has(header.id)) refuse(`Duplicate native POUR ID ${header.id}; inventory is ambiguous.`);
		ids.add(header.id);
		if (!Number.isSafeInteger(payload.layerId) || Number(payload.layerId) <= 0
			|| !Number.isSafeInteger(payload.order) || Number(payload.order) < 0) refuse('Native POUR layer/order must be explicit nonnegative integers.');
		if (typeof payload.netName !== 'string' || !payload.netName.trim()
			|| typeof payload.width !== 'number' || !Number.isFinite(payload.width) || payload.width <= 0
			|| typeof payload.name !== 'string' || !payload.name
			|| !Array.isArray(payload.path) || !payload.path.length
			|| payload.path.some(path => !Array.isArray(path) || !path.length)
			|| !object(payload.pourType) || typeof payload.pourType.pourType !== 'string'
			|| typeof payload.keepIsland !== 'boolean' || typeof payload.locked !== 'boolean') {
			refuse('Native POUR required fields are missing or unsupported; inventory is unavailable.');
		}
		pours.push({ primitiveId: header.id, layer: Number(payload.layerId), order: Number(payload.order), payload });
	}
	if (docheads !== 1) refuse('Native PCB DOCHEAD is unavailable; do not treat missing source as an empty inventory.');
	return freeze({ source, pours });
}

async function exactContext(target?: PourOrderTarget): Promise<PourOrderTarget> {
	const context = await readResponseContext();
	if (context.documentType !== 'pcb' || typeof context.projectUuid !== 'string' || !context.projectUuid.trim()
		|| typeof context.documentUuid !== 'string' || !context.documentUuid.trim()) refuse('Exact PCB project/document identity is unavailable.');
	if (target && (context.projectUuid !== target.projectUuid || context.documentUuid !== target.documentUuid)) {
		refuse('Active project/document changed during native pour-order verification.');
	}
	return { projectUuid: context.projectUuid, documentUuid: context.documentUuid, documentType: 'pcb' };
}
async function readInventory(target: PourOrderTarget): Promise<NativePourInventory> {
	await exactContext(target);
	const response = await documentSourceGet({ projectUuid: target.projectUuid, documentUuid: target.documentUuid });
	const result = response.result;
	if (!result || result.verified !== true || result.partial !== false || result.incomplete !== false
		|| !object(result.target) || result.target.projectUuid !== target.projectUuid
		|| result.target.documentUuid !== target.documentUuid || result.target.documentType !== 'pcb') {
		if (typeof result?.source === 'string') throw new NativePourSourceError('Identity-bound native PCB source read was not confirmed.', result.source);
		refuse('Identity-bound native PCB source read was not confirmed.');
	}
	try { return parseNativePourOrderSource(result.source, target); }
	catch (error) {
		if (typeof result.source === 'string') throw new NativePourSourceError(describeThrown(error), result.source);
		throw error;
	}
}

/** The caller must prepare before create. Omission deliberately does not read
 * source or alter the host-default workflow. Explicit order means native
 * zero-based rank under a proven contiguous baseline, not the SDK getter. */
export async function preparePourOrder(priority: unknown, layer: number): Promise<PourOrderPlan | undefined> {
	if (priority === undefined) return undefined;
	if (!Number.isSafeInteger(priority) || Number(priority) < 0) refuse('priority must be an explicit nonnegative safe integer (native order).');
	if (!Number.isSafeInteger(layer) || layer <= 0) refuse('Pour-order target layer must be a positive integer.');
	const api = typeof eda === 'undefined' ? undefined : eda.pcb_PrimitivePour;
	if (typeof api?.create !== 'function' || typeof api?.getAll !== 'function') {
		throw new ActionError(ErrorCodes.EDA_API_UNAVAILABLE, 'Public pour create/getAll API is unavailable; no pour was created.');
	}
	const target = await exactContext();
	const baseline = await readInventory(target);
	const layerPours = baseline.pours.filter(pour => pour.layer === layer).sort((a, b) => a.order - b.order);
	if (layerPours.some((pour, index) => pour.order !== index)) refuse('Existing target-layer native orders must be unique and contiguous 0..N-1; no pour was created.');
	if (Number(priority) > layerPours.length) refuse(`priority is outside native insertion order 0..${layerPours.length}; no pour was created.`);
	return freeze({ target, layer, requested: Number(priority), baseline });
}

interface WritablePour {
	getState_PrimitiveId(): string;
	getState_Layer(): number;
	getState_PourPriority?(): unknown;
	toAsync(): WritablePour;
	isAsync(): boolean;
	setState_PourPriority(value: number): WritablePour;
	done(): Promise<unknown>;
}
function oldInventoryUnchanged(plan: PourOrderPlan, inventory: NativePourInventory, primitiveId: string): NativePourOrder {
	if (inventory.pours.length !== plan.baseline.pours.length + 1) refuse('Created pour inventory has missing or unexpected objects.');
	for (const old of plan.baseline.pours) {
		const current = inventory.pours.find(pour => pour.primitiveId === old.primitiveId);
		if (!current || canonical(current.payload) !== canonical(old.payload)) refuse(`Baseline pour ${old.primitiveId} changed before order application.`);
	}
	const created = inventory.pours.find(pour => pour.primitiveId === primitiveId);
	if (!created || created.layer !== plan.layer) refuse('Created pour is missing or on a different native layer.');
	const count = plan.baseline.pours.filter(pour => pour.layer === plan.layer).length;
	if (created.order !== count) refuse('Created host-default native order does not follow the frozen baseline.');
	return created;
}
function verifiedInventory(plan: PourOrderPlan, inventory: NativePourInventory, createdBefore: NativePourOrder): NativePourOrder {
	if (inventory.pours.length !== plan.baseline.pours.length + 1) refuse('Final pour inventory has missing or unexpected objects.');
	for (const old of plan.baseline.pours) {
		const current = inventory.pours.find(pour => pour.primitiveId === old.primitiveId);
		const expectedOrder = old.layer === plan.layer && old.order >= plan.requested ? old.order + 1 : old.order;
		if (!current || current.layer !== old.layer || current.order !== expectedOrder
			|| withoutOrder(current.payload) !== withoutOrder(old.payload)) refuse(`Pour ${old.primitiveId} differs from the expected complete native readback.`);
	}
	const created = inventory.pours.find(pour => pour.primitiveId === createdBefore.primitiveId);
	if (!created || created.layer !== plan.layer || created.order !== plan.requested
		|| withoutOrder(created.payload) !== withoutOrder(createdBefore.payload)) refuse('Created pour native order or non-order fields differ from the requested insertion.');
	return created;
}

/** The caller supplies a fresh getAll object, never the create echo. Its class
 * constructor is not a public runtime global, so empty-layer instance methods
 * cannot be proven before create. Missing methods here are accountable partial
 * failure, with no order write. No timeout race, retry, or rollback is added. */
export async function applyAndVerifyPourOrder(plan: PourOrderPlan, freshCreated: unknown): Promise<PourOrderVerification> {
	const fresh = freshCreated as Partial<WritablePour> | null;
	let primitiveId = 'unknown';
	try {
		if (typeof fresh?.getState_PrimitiveId === 'function') primitiveId = fresh.getState_PrimitiveId();
	} catch { /* retain unknown identity as an explicit failure */ }
	const evidence: PourOrderEvidence = { primitiveId, requested: plan.requested, orderWriteAttempted: false, baselineSource: plan.baseline.source };
	let readingAfterWrite = false;
	try {
		if (!/^[0-9a-f]{16}$/.test(primitiveId) || plan.baseline.pours.some(pour => pour.primitiveId === primitiveId)) refuse('Fresh created primitive identity is missing or collides with the baseline.');
		for (const method of ['getState_Layer', 'toAsync', 'isAsync', 'setState_PourPriority', 'done'] as const) {
			if (typeof fresh?.[method] !== 'function') refuse(`Public created-pour method ${method} is unavailable; no order write was attempted.`);
		}
		const writable = fresh as WritablePour;
		if (writable.getState_Layer() !== plan.layer) refuse('Fresh created pour belongs to a different layer.');
		if (typeof writable.getState_PourPriority === 'function') {
			try { evidence.sdkPriorityBefore = writable.getState_PourPriority(); }
			catch (error) { evidence.sdkPriorityReadError = describeThrown(error); }
		}
		const before = await readInventory(plan.target);
		evidence.beforeOrderSource = before.source;
		const createdBefore = oldInventoryUnchanged(plan, before, primitiveId);
		evidence.beforeNativeOrder = createdBefore.order;
		await exactContext(plan.target);
		if (writable.toAsync() !== writable || writable.isAsync() !== true) refuse('Created-pour asynchronous mode was not confirmed; no order write was attempted.');
		writable.setState_PourPriority(plan.requested);
		// Public API modify() starts done() without awaiting it. Explicitly await
		// this one instance done() so the connector FIFO retains the write.
		evidence.orderWriteAttempted = true;
		await writable.done();
		readingAfterWrite = true;
		const after = await readInventory(plan.target);
		evidence.afterOrderSource = after.source;
		evidence.afterNativeOrder = after.pours.find(pour => pour.primitiveId === primitiveId)?.order;
		const created = verifiedInventory(plan, after, createdBefore);
		return { ...evidence, verified: true, nativeOrder: created.order, target: plan.target,
			finalOrders: after.pours.map(pour => ({ primitiveId: pour.primitiveId, layer: pour.layer, order: pour.order })) };
	} catch (error) {
		if (error instanceof NativePourSourceError) {
			if (readingAfterWrite) evidence.afterOrderSource = error.source;
			else evidence.beforeOrderSource = error.source;
		}
		throw new PourOrderError(describeThrown(error), evidence);
	}
}

/** Read-only final verification after the caller's rebuild. The earlier proof
 * cannot certify a later boundary/native state. Preserve both snapshots and
 * require the same insertion mapping without sending another setter. */
export async function verifyPourOrderReadback(plan: PourOrderPlan, proof: PourOrderVerification): Promise<PourOrderVerification> {
	const evidence: PourOrderEvidence = { primitiveId: proof.primitiveId, requested: plan.requested,
		orderWriteAttempted: proof.orderWriteAttempted, baselineSource: proof.baselineSource,
		beforeOrderSource: proof.beforeOrderSource, afterOrderSource: proof.afterOrderSource,
		sdkPriorityBefore: proof.sdkPriorityBefore, sdkPriorityReadError: proof.sdkPriorityReadError,
		beforeNativeOrder: proof.beforeNativeOrder, afterNativeOrder: proof.afterNativeOrder };
	try {
		if (proof.verified !== true || proof.requested !== plan.requested || proof.nativeOrder !== plan.requested
			|| proof.baselineSource !== plan.baseline.source || !proof.beforeOrderSource
			|| proof.target.projectUuid !== plan.target.projectUuid || proof.target.documentUuid !== plan.target.documentUuid
			|| proof.target.documentType !== 'pcb') refuse('Earlier pour-order proof does not match the frozen plan.');
		const before = parseNativePourOrderSource(proof.beforeOrderSource, plan.target);
		const createdBefore = oldInventoryUnchanged(plan, before, proof.primitiveId);
		const after = await readInventory(plan.target);
		evidence.afterRebuildSource = after.source;
		evidence.afterNativeOrder = after.pours.find(pour => pour.primitiveId === proof.primitiveId)?.order;
		const created = verifiedInventory(plan, after, createdBefore);
		return { ...proof, ...evidence, verified: true, nativeOrder: created.order,
			finalOrders: after.pours.map(pour => ({ primitiveId: pour.primitiveId, layer: pour.layer, order: pour.order })) };
	} catch (error) {
		if (error instanceof NativePourSourceError) evidence.afterRebuildSource = error.source;
		throw new PourOrderError(describeThrown(error), evidence);
	}
}
