import type { MeetingStatus } from "../api/client";

export const STATUS_LABEL: Record<MeetingStatus, string> = {
  recording: "Waiting for audio",
  queued: "Queued",
  transcribing: "Transcribing",
  summarizing: "Summarizing",
  ready: "Ready",
  failed: "Failed",
};

export type Tone = "neutral" | "progress" | "good" | "bad";

export function statusTone(status: MeetingStatus): Tone {
  if (status === "ready") return "good";
  if (status === "failed") return "bad";
  if (status === "recording") return "neutral";
  return "progress";
}

export function isProcessing(status?: MeetingStatus): boolean {
  return status !== undefined && statusTone(status) === "progress";
}

export function pollInterval(status?: MeetingStatus): number | false {
  return isProcessing(status) ? 2000 : false;
}

export function noSpeech(status?: MeetingStatus, segments?: readonly unknown[]): boolean {
  return status === "ready" && segments?.length === 0;
}

export function hasTranscript(status?: MeetingStatus): boolean {
  return status === "summarizing" || status === "ready" || status === "failed";
}
