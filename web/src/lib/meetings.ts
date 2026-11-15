import type { Meeting, MeetingDetail, Summary } from "../api/client";
import { formatDuration } from "./format";
import { isProcessing } from "./status";

export type Filter = "all" | "ready" | "processing" | "failed";

export const FILTERS: { value: Filter; label: string }[] = [
  { value: "all", label: "All meetings" },
  { value: "ready", label: "Ready" },
  { value: "processing", label: "Processing" },
  { value: "failed", label: "Failed" },
];

export function parseFilter(value: string | null): Filter {
  return FILTERS.some((f) => f.value === value) ? (value as Filter) : "all";
}

export function matchesFilter(filter: Filter, m: Meeting): boolean {
  if (filter === "all") return true;
  if (filter === "processing") return isProcessing(m.status);
  return m.status === filter;
}

export function meetingDate(m: Meeting): string {
  return m.started_at ?? m.created_at;
}

// groupByDay keeps the input order and starts a new group whenever the local calendar day changes.
export function groupByDay(items: Meeting[]): { key: string; label: string; items: Meeting[] }[] {
  const groups: { key: string; label: string; items: Meeting[] }[] = [];
  for (const m of items) {
    const d = new Date(meetingDate(m));
    const key = d.toDateString();
    if (groups.at(-1)?.key !== key) {
      groups.push({ key, label: d.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" }), items: [] });
    }
    groups.at(-1)!.items.push(m);
  }
  return groups;
}

export function initial(name: string): string {
  return (name.trim().match(/\p{L}|\p{N}/u)?.[0] ?? "?").toUpperCase();
}

// paletteIndex picks a stable color slot for a name, so the same meeting keeps its avatar color.
export function paletteIndex(name: string, size: number): number {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return h % size;
}

// missingSummary explains why a ready meeting has no summary.
export function missingSummary(m: Pick<MeetingDetail, "summary_error">): string {
  return m.summary_error ? `No summary yet: ${m.summary_error}` : "No summary: no speech was recognized in this recording.";
}

export function summaryByline(s: Summary): string {
  if (s.provider !== "claude") return "Written by the rule-based summarizer";
  const tokens = s.input_tokens != null && s.output_tokens != null ? ` · ${s.input_tokens.toLocaleString("en")} in, ${s.output_tokens.toLocaleString("en")} out tokens` : "";
  return `Written by Claude (${s.model ?? "unknown model"})${tokens}`;
}

export function meetingMeta(m: Meeting): string {
  const d = new Date(meetingDate(m));
  const parts = [
    d.toLocaleDateString(undefined, { month: "short", day: "numeric" }),
    d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" }),
  ];
  if (m.duration_ms != null) parts.push(formatDuration(m.duration_ms));
  if (m.participants.length) parts.push(m.participants.join(", "));
  return parts.join(" · ");
}
