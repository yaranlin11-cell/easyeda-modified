package app

// schPinRootedWireOrder grows each wire tree from measured pins. Some hosts
// reject an initially detached segment even when the complete planned tree is
// valid. Only creation order changes: geometry, nets and all later gates stay
// intact. Unrooted remainders retain their original order, not silently vanish.
func schPinRootedWireOrder(parts []powerLayoutPlacement, wires []powerLayoutWire) []powerLayoutWire {
	anchors := map[string][][2]float64{}
	for _, part := range parts {
		for _, pin := range part.Pins {
			anchors[pin.Net] = append(anchors[pin.Net], [2]float64{pin.X, pin.Y})
		}
	}
	remaining := append([]powerLayoutWire(nil), wires...)
	ordered := make([]powerLayoutWire, 0, len(wires))
	installed := map[string][][4]float64{}
	for len(remaining) > 0 {
		chosen := -1
		for i, wire := range remaining {
			for j := 1; j < len(wire.Points) && chosen < 0; j++ {
				a, b := wire.Points[j-1], wire.Points[j]
				for _, anchor := range anchors[wire.Net] {
					if pointOnSeg(anchor[0], anchor[1], a[0], a[1], b[0], b[1]) {
						chosen = i
						break
					}
				}
			}
			for _, endpoint := range wire.Points {
				for _, segment := range installed[wire.Net] {
					if pointOnSeg(endpoint[0], endpoint[1], segment[0], segment[1], segment[2], segment[3]) {
						chosen = i
					}
				}
			}
			if chosen >= 0 {
				break
			}
		}
		if chosen < 0 {
			return append(ordered, remaining...)
		}
		wire := remaining[chosen]
		ordered = append(ordered, wire)
		anchors[wire.Net] = append(anchors[wire.Net], wire.Points...)
		for j := 1; j < len(wire.Points); j++ {
			a, b := wire.Points[j-1], wire.Points[j]
			installed[wire.Net] = append(installed[wire.Net], [4]float64{a[0], a[1], b[0], b[1]})
		}
		remaining = append(remaining[:chosen], remaining[chosen+1:]...)
	}
	return ordered
}
