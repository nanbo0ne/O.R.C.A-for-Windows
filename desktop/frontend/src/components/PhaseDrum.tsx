import { useEffect, useRef, useState, type CSSProperties } from "react";
import { drumFaces, forwardSector, phaseSector } from "../lib/phaseDrum";
import { workMonitorPhase } from "../lib/workMonitorLabels";

export function PhaseDrum({ phase, lastActivePhase, locale, generation, unavailable = false }: {
  phase?: string; lastActivePhase?: string; locale: string; generation: string; unavailable?: boolean;
}) {
  const initial = phaseSector(phase) ?? 0;
  const [position, setPosition] = useState(initial);
  const motion = useRef({ position: initial, target: initial, generation, observed: false });
  const [observed, setObserved] = useState(false);
  const [facePhases, setFacePhases] = useState<Record<number, string>>({});
  useEffect(() => {
    const m = motion.current;
    const sector = phaseSector(phase) ?? (!m.observed || m.generation !== generation ? phaseSector(lastActivePhase) : null);
    const fresh = m.generation !== generation;
    // Keep the outgoing face's wording while the next face moves into view.
    const activePhase = phaseSector(phase) !== null ? phase : lastActivePhase;
    if (sector !== null && activePhase) setFacePhases(previous => ({ ...(fresh ? {} : previous), [sector]: activePhase }));
    else if (fresh) setFacePhases({});
    if (m.generation !== generation) {
      Object.assign(m, { position: sector ?? 0, target: sector ?? 0, generation, observed: false });
      setPosition(m.position);
      setObserved(false);
    }
    if (sector !== null && !m.observed) {
      m.observed = true;
      m.position = m.target = sector;
      setPosition(sector);
      setObserved(true);
      return;
    }
    if (!m.observed) return;
    // Pause/stop/disconnection freezes the surface exactly where it is.
    if (sector === null || unavailable) return;
    // Coalesce bursts from the rendered surface, never queue extra revolutions.
    m.target = forwardSector(m.position, sector);
    const media = matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0, start = performance.now();
    const from = m.position, target = m.target;
    const duration = Math.min(620, 380 + (target - from) * 60);
    const finish = () => {
      cancelAnimationFrame(frame);
      m.position = target;
      setPosition(target);
    };
    const reduced = () => { if (media.matches) finish(); };
    const tick = (time: number) => {
      const t = Math.max(0, Math.min(1, (time - start) / duration));
      // Monotonic ease-out: light momentum without overshooting or reversing.
      m.position = from + (target - from) * (1 - (1 - t) ** 3);
      setPosition(m.position);
      if (t < 1) frame = requestAnimationFrame(tick);
    };
    if (media.matches || document.hidden || target === from) finish();
    else frame = requestAnimationFrame(tick);
    media.addEventListener("change", reduced);
    return () => { cancelAnimationFrame(frame); media.removeEventListener("change", reduced); };
  }, [phase, lastActivePhase, generation, unavailable]);
  const zh = locale === "zh";
  const phaseLabel = (value: string) => value === "wait-first" ? (zh ? "等待首片段" : "Waiting") : workMonitorPhase(value, locale);
  const label = unavailable ? (zh ? "观测已中断" : "Observation interrupted")
    : phase ? phaseLabel(phase) : (zh ? "尚未观测" : "Not observed");
  const frozen = unavailable || phaseSector(phase) === null;
  const description = zh ? "相邻颜色仅表示固定阶段位置，不代表已发生或预测的操作。" : "Adjacent colors are fixed phase positions, not observed or predicted actions.";
  return <div className={`phase-drum${observed ? " phase-drum--observed" : ""}`}
    role="img" aria-label={label} tabIndex={0} title={`${label}. ${description}`}
    data-phase={phase ?? "unknown"} data-position={position}>
    <span className="phase-drum__pointer" aria-hidden="true" />
    <div className="phase-drum__frame" aria-hidden="true">
    <div className="phase-drum__window">
      {observed && drumFaces(position).map(face => <span key={face.slot}
        className={`phase-drum__face phase-drum__face--${face.sector}`}
        style={{ left: `${face.left}%`, width: `${face.width}%`,
          "--phase-label-opacity": frozen ? (face.slot === Math.round(position) ? 1 : 0) : Math.max(0, 1 - Math.abs(face.slot - position)) } as CSSProperties}>
        <span className="phase-drum__label">{frozen && face.slot === Math.round(position) ? label
          : phaseLabel(facePhases[face.sector] ?? ["input", "wait-first", "reasoning", "decode"][face.sector])}</span>
      </span>)}
      {!observed && <span className="phase-drum__label">{label}</span>}
    </div>
    </div>
  </div>;
}
