export const phaseSector = (phase?: string): number | null => {
  switch (phase) {
    case "input": case "tool": return 0;
    case "wait": case "wait-first": return 1;
    case "reasoning": return 2;
    case "decode": case "final": return 3;
    default: return null;
  }
};
export const sectorIndex = (position: number) => ((position % 4) + 4) % 4;
export function forwardSector(target: number, sector: number): number {
  return target + sectorIndex(sector - sectorIndex(target));
}

// Flattened side projection of four quadrants. A half circumference
// intersects at most three quadrants; adjacent faces are not predicted events.
export function drumFaces(position: number) {
  const faces: { slot: number; sector: number; left: number; width: number }[] = [];
  const project = (angle: number) => {
    const s = Math.sin(angle);
    // Keep both ends fixed while shortening the centred face by ten percent.
    return Math.max(0, Math.min(100, (1 + 0.8 * s + 0.2 * s ** 3) * 50));
  };
  for (let slot = Math.floor(position) - 1; slot <= Math.ceil(position) + 1; slot++) {
    const centre = (slot - position) * Math.PI / 2;
    const leftAngle = Math.max(-Math.PI / 2, centre - Math.PI / 4);
    const rightAngle = Math.min(Math.PI / 2, centre + Math.PI / 4);
    if (rightAngle <= leftAngle) continue;
    const left = project(leftAngle);
    const right = project(rightAngle);
    faces.push({ slot, sector: sectorIndex(slot), left, width: right - left });
  }
  return faces;
}
