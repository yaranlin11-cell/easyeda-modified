# Native PCB pad geometry regression

Source: synthetic, de-identified reproduction of the snapshot projection defect;
shape tuple semantics follow the official EasyEDA Pro SDK. All coordinates and
sizes are mil. No customer project or device library identities are required.

Start state: a TOP component owns a TOP RECT 20×20 pad at (0,0); a BOTTOM
component owns a MULTI RECT 20×20 pad at (18,18), on a different net. They share
TOP copper and overlap by 2×2 mil even though their component bodies are on
opposite assembly sides. Parameters are pad shape, size, center, rotation, net
and copper layer; change these fields to recompute rather than copy a board.

Run `easyeda pcb layout-score --from internal/app/testdata/pcb-native-geometry/square-corner.json --only clearance --min-gap 10 --min-score 99 --json --all`.
The old projection dropped shape and misclassified the square as a circle,
returning ok:true/100/exit 0. The repaired command must report the copper short
and return nonzero. `TestLayoutSquareCornerFixtureRejectsThreshold` reads this
file; native circle, rounded rectangle and disjoint-layer controls are covered
by `TestLayoutNativeRectCornerContactAndCircleControl`.

Validation: offline CLI/geometry regression; this fixture is not an EDA design,
manufacturing or full-board E2E acceptance result.
