import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { useT } from "../lib/i18n";
import type { JobView } from "../lib/types";

export interface OverviewSection {
  key: string;
  label: string;
  icon: ReactNode;
  summary?: string;
  badge?: number;
  disabled?: boolean;
  content: ReactNode;
}

// The right pane is a stack of sections rather than a tab strip: each header
// states what the section holds while it is folded, and expanding it shows the
// real panel in place. One section is open at a time so the pane never has to
// scroll to find the one that matters.
export function OverviewDock({
  sections,
  active,
  onSelect,
}: {
  sections: OverviewSection[];
  active: string;
  onSelect: (key: string) => void;
}) {
  return (
    <div className="overview-dock">
      {sections.map((section) => {
        const open = section.key === active;
        return (
          <section className={`overview-dock__section${open ? " is-open" : ""}`} key={section.key}>
            <button
              type="button"
              className="overview-dock__head"
              aria-expanded={open}
              disabled={section.disabled}
              onClick={() => onSelect(section.key)}
            >
              <span className="overview-dock__icon">{section.icon}</span>
              <span className="overview-dock__title">{section.label}</span>
              {section.summary && !open && <span className="overview-dock__summary">{section.summary}</span>}
              {typeof section.badge === "number" && section.badge > 0 && <span className="overview-dock__badge">{section.badge}</span>}
              <ChevronDown size={14} className="overview-dock__chevron" />
            </button>
            {open && <div className="overview-dock__body">{section.content}</div>}
          </section>
        );
      })}
    </div>
  );
}

function elapsedLabel(startedAt: number): string {
  const seconds = Math.max(0, Math.round((Date.now() - startedAt) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

// Background jobs get their own pane: they outlive the turn that started them,
// so a running job must stay visible while the conversation moves on.
export function BackgroundJobsPanel({ jobs }: { jobs?: JobView[] }) {
  const t = useT();
  const list = Array.isArray(jobs) ? jobs : [];
  return (
    <div className="background-jobs-panel">
      <header className="background-jobs-panel__head">
        <h2>{t("process.timeline.background")}</h2>
        <span>{t("process.timeline.backgroundCount", { n: list.length })}</span>
      </header>
      {list.length === 0 ? (
        <div className="background-jobs-panel__empty">{t("background.empty")}</div>
      ) : (
        <ul className="background-jobs-panel__list">
          {list.map((job) => (
            <li key={job.id} className="background-jobs-panel__row">
              <span className={`background-jobs-panel__dot background-jobs-panel__dot--${job.status || "running"}`} aria-hidden="true" />
              <span className="background-jobs-panel__label" title={job.id}>{job.label || job.id}</span>
              <span className="background-jobs-panel__meta">{job.kind} · {elapsedLabel(job.startedAt)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
