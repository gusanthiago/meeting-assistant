import type React from "react";
import type { MeetingStatus, Segment, SnippetPart } from "../api/client";
import { formatClock } from "../lib/format";
import { initial, paletteIndex } from "../lib/meetings";
import { STATUS_LABEL, statusTone, type Tone } from "../lib/status";

const buttonBase =
  "inline-flex items-center justify-center gap-2 rounded-full px-4 py-2 text-sm font-medium no-underline transition cursor-pointer disabled:cursor-default disabled:opacity-50";

export const btn = {
  primary: `${buttonBase} bg-accent text-white hover:bg-accent-hover`,
  secondary: `${buttonBase} border border-line bg-surface text-ink hover:bg-raised`,
  ghost: `${buttonBase} text-muted hover:bg-raised hover:text-ink`,
};

export const roundBtn = {
  primary: "inline-flex size-14 items-center justify-center rounded-full bg-accent text-white transition hover:bg-accent-hover cursor-pointer disabled:opacity-50",
  secondary: "inline-flex size-14 items-center justify-center rounded-full border border-line bg-raised text-ink transition hover:bg-line cursor-pointer",
  danger: "inline-flex size-14 items-center justify-center rounded-full bg-red-500 text-white transition hover:bg-red-400 cursor-pointer",
};

export const card = "rounded-xl border border-line bg-surface";
export const tag = "rounded-full border border-line bg-raised/80 px-3 py-1 text-xs text-ink/85";
export const input = "w-full rounded-lg border border-line bg-panel px-3 py-2 text-ink outline-none transition focus:border-accent";
export const errorBox = "rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-red-200";
export const warningBox = "flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-amber-100";
export const sectionTitle = "text-xs font-medium uppercase tracking-wider text-muted";
export const timestampClass = "font-mono text-xs tabular-nums text-muted no-underline transition hover:text-ink cursor-pointer";

const toneClass: Record<Tone, string> = {
  neutral: "bg-raised text-muted",
  progress: "bg-accent-soft text-violet-200",
  good: "bg-emerald-500/15 text-emerald-300",
  bad: "bg-red-500/15 text-red-300",
};

export function StatusBadge({ status }: { status: MeetingStatus }) {
  const tone = statusTone(status);
  return (
    <span data-tone={tone} className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-md px-2 py-0.5 text-xs font-medium ${toneClass[tone]}`}>
      {tone === "progress" && <span className="size-1.5 animate-pulse rounded-full bg-current motion-reduce:animate-none" aria-hidden="true" />}
      {STATUS_LABEL[status]}
    </span>
  );
}

export function Eyebrow({ badge, children }: { badge: string; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-2 justify-self-start rounded-full border border-line bg-surface/80 py-1 pr-3 pl-1 text-xs text-ink/90">
      <span className="rounded-full bg-accent px-2 py-0.5 font-medium text-white">{badge}</span>
      {children}
    </span>
  );
}

const avatarColors = ["bg-blue-600", "bg-emerald-600", "bg-amber-600", "bg-rose-600", "bg-violet-600", "bg-cyan-600"];

export function Avatar({ name, size = "md" }: { name: string; size?: "sm" | "md" | "lg" }) {
  const dims = { sm: "size-6 text-[11px] rounded-md", md: "size-11 text-lg rounded-lg", lg: "size-14 text-2xl rounded-xl" }[size];
  return (
    <span className={`inline-flex shrink-0 items-center justify-center font-medium text-white ${dims} ${avatarColors[paletteIndex(name, avatarColors.length)]}`} aria-hidden="true">
      {initial(name)}
    </span>
  );
}

export function SnippetText({ parts }: { parts: SnippetPart[] }) {
  return (
    <>
      {parts.map((p, i) => (p.match ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>))}
    </>
  );
}

export function TranscriptList({ segments, activeMs, onSeek }: { segments: Segment[]; activeMs: number; onSeek: (ms: number) => void }) {
  return (
    <ol className="grid">
      {segments.map((s, i) => {
        const active = activeMs >= s.start_ms && activeMs < s.end_ms;
        const newSpeaker = i === 0 || segments[i - 1].speaker !== s.speaker;
        return (
          <li key={i} data-active={active} className={`grid grid-cols-[56px_1fr] gap-3 rounded-lg px-3 py-2 transition ${active ? "bg-accent-soft" : "hover:bg-raised/60"}`}>
            <button type="button" className={`${timestampClass} self-start pt-0.5 text-left`} onClick={() => onSeek(s.start_ms)} aria-label={`Play from ${formatClock(s.start_ms)}`}>
              {formatClock(s.start_ms)}
            </button>
            <div className="min-w-0">
              {newSpeaker && <div className="mb-0.5 text-xs font-semibold text-violet-200">{s.speaker}</div>}
              <p className="leading-relaxed text-ink/90">{s.text}</p>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
