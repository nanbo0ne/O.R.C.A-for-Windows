import assert from "node:assert/strict";
import { drumFaces, forwardSector, phaseSector } from "../lib/phaseDrum";

for (let n = -400; n <= 400; n++) {
  const position = n / 19;
  const faces = drumFaces(position);
  assert.ok(faces.length <= 3 && faces.length >= 2);
  assert.ok(Math.abs(faces.reduce((n, face) => n + face.width, 0) - 100) < 1e-8);
  for (const face of faces) assert.ok(face.width > 0 && face.left >= 0 && face.left + face.width <= 100 + 1e-8);
}
for (let target = 0; target < 100; target++) for (let sector = 0; sector < 4; sector++) {
  const next = forwardSector(target, sector);
  assert.ok(next >= target && next - target < 4);
  assert.equal(next % 4, sector);
  const centre = drumFaces(next).find(face => face.left <= 50 && face.left + face.width >= 50)!;
  assert.equal(centre.sector, sector);
  assert.ok(Math.abs(centre.width - Math.SQRT1_2 * 100 * 0.9) < 1e-8);
}
assert.equal(phaseSector("tool"), 0);
assert.equal(phaseSector("wait-first"), 1);
assert.equal(phaseSector("reasoning"), 2);
assert.equal(phaseSector("decode"), 3);
assert.equal(phaseSector("final"), 3);
assert.ok(Math.abs(forwardSector(2.3, 0) - 4) < 1e-8);
assert.ok(Math.abs(forwardSector(2.3, 2) - 6) < 1e-8);
for (const state of [undefined, "paused", "stopped", "error", "cancelling"]) assert.equal(phaseSector(state), null);
console.log("PASS phase drum projection, three-color bound, forward-only motion and truthful phase mapping");
