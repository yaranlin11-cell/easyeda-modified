/** Hard geometry facts are independent of the placement preference score. */
export type SilkRect = { minX: number; minY: number; maxX: number; maxY: number };
export type SilkObstacle = { rect: SilkRect; kind: string; owner: string; m: number; layer?: number };
export type SilkLabel = { rect: SilkRect; layer: number };

export function validSilkRect(r: unknown): r is SilkRect {
	if (!r || typeof r !== 'object') return false;
	const b = r as SilkRect;
	return [b.minX, b.minY, b.maxX, b.maxY].every(v => typeof v === 'number' && Number.isFinite(v))
		&& b.minX < b.maxX && b.minY < b.maxY;
}

export function silkSlotFacts(rect: SilkRect, owner: string, attribute: string, layer: number,
	obstacles: SilkObstacle[], labels: Record<string, SilkLabel>, safeArea: SilkRect | null, labelMargin: number) {
	const overlap = (a: SilkRect, b: SilkRect, m: number) => a.minX < b.maxX + m && a.maxX > b.minX - m
		&& a.minY < b.maxY + m && a.maxY > b.minY - m;
	const conflicts: Array<{ kind: string; owner: string }> = [];
	let softRegions = 0, minClearance = Infinity;
	if (safeArea && !(rect.minX >= safeArea.minX && rect.maxX <= safeArea.maxX
		&& rect.minY >= safeArea.minY && rect.maxY <= safeArea.maxY)) conflicts.push({ kind: 'OUTLINE_BOUNDS', owner: '' });
	for (const o of obstacles) {
		if (o.layer != null && o.layer !== layer) continue;
		if (overlap(rect, o.rect, o.m)) {
			if (o.kind === 'REGION_S') softRegions++;
			else conflicts.push({ kind: o.kind, owner: o.owner }); // includes the owner's complete envelope
		}
		const dx = Math.max(0, rect.minX - o.rect.maxX, o.rect.minX - rect.maxX);
		const dy = Math.max(0, rect.minY - o.rect.maxY, o.rect.minY - rect.maxY);
		minClearance = Math.min(minClearance, Math.hypot(dx, dy));
	}
	for (const [id, label] of Object.entries(labels)) {
		if (id === attribute || label.layer !== layer) continue;
		if (overlap(rect, label.rect, labelMargin)) conflicts.push({ kind: 'LABEL', owner: id });
		const dx = Math.max(0, rect.minX - label.rect.maxX, label.rect.minX - rect.maxX);
		const dy = Math.max(0, rect.minY - label.rect.maxY, label.rect.minY - rect.maxY);
		minClearance = Math.min(minClearance, Math.hypot(dx, dy));
	}
	return { clean: conflicts.length === 0, conflicts, softRegions, minClearance };
}

/** Search along each side as well as its midpoint, without changing any clearance. */
export function silkSideSlots(body: SilkRect, width: number, height: number, distance: number,
 directions: number[][]): Array<{ x: number; y: number; rank: number }> {
 const cx = (body.minX + body.maxX) / 2, cy = (body.minY + body.maxY) / 2;
 const hw = (body.maxX - body.minX) / 2, hh = (body.maxY - body.minY) / 2;
 const slots: Array<{ x: number; y: number; rank: number }> = [];
 for (let rank = 0; rank < directions.length; rank++) {
  const [dx, dy] = directions[rank];
  const x = cx + dx * (hw + distance + width / 2), y = cy + dy * (hh + distance + height / 2);
  // Diagonal seeds already move along both axes. Side seeds also explore the
  // finite span of their own component, instead of jumping past a neighbour.
  const span = dx === 0 ? hw : dy === 0 ? hh : 0;
  for (const shift of span ? [0, -span / 2, span / 2, -span, span] : [0]) {
   slots.push({ x: x + (dx === 0 ? shift : 0), y: y + (dy === 0 ? shift : 0), rank: Math.min(rank, 3) });
  }
 }
 return slots;
}
