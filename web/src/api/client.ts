import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

type Schemas = components["schemas"];
export type Meeting = Schemas["Meeting"];
export type MeetingDetail = Schemas["MeetingDetail"];
export type MeetingStatus = Schemas["MeetingStatus"];
export type Transcript = Schemas["Transcript"];
export type Segment = Schemas["Segment"];
export type ActionItem = Schemas["ActionItem"];
export type Summary = Schemas["Summary"];
export type SearchHit = Schemas["SearchHit"];
export type Snippet = Schemas["Snippet"];
export type SnippetPart = Schemas["SnippetPart"];

export const api = createClient<paths>({ baseUrl: "" });

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

// Every JSON response is an envelope: {code, status, data} or {code, status, error}.
function unwrap<T>(res: { data?: { data: T }; error?: { error?: string }; response: Response }): T {
  if (res.data) return res.data.data;
  throw new ApiError(res.error?.error || `Request failed (${res.response.status})`, res.response.status);
}

export async function listMeetings(cursor?: string) {
  return unwrap(await api.GET("/v1/meetings", { params: { query: { limit: 20, cursor } } }));
}

export async function getMeeting(id: string) {
  return unwrap(await api.GET("/v1/meetings/{meetingId}", { params: { path: { meetingId: id } } }));
}

export async function getTranscript(id: string): Promise<Transcript | null> {
  const res = await api.GET("/v1/meetings/{meetingId}/transcript", { params: { path: { meetingId: id } } });
  if (res.response.status === 404) return null;
  return unwrap(res);
}

export async function createMeeting(title: string, participants: string[], idempotencyKey: string) {
  return unwrap(
    await api.POST("/v1/meetings", {
      params: { header: { "Idempotency-Key": idempotencyKey } },
      body: { title, participants, started_at: new Date().toISOString() },
    }),
  );
}

export async function uploadRecording(id: string, audio: Blob, durationMs?: number): Promise<Meeting> {
  const query = durationMs ? `?duration_ms=${Math.round(durationMs)}` : "";
  const res = await fetch(`/v1/meetings/${id}/recording${query}`, {
    method: "PUT",
    headers: { "Content-Type": audio.type || "audio/webm" },
    body: audio,
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(body.error || `Upload failed (${res.status})`, res.status);
  return body.data as Meeting;
}

export async function regenerateSummary(id: string) {
  return unwrap(await api.POST("/v1/meetings/{meetingId}/summary/regenerate", { params: { path: { meetingId: id } } }));
}

export async function retryMeeting(id: string) {
  return unwrap(await api.POST("/v1/meetings/{meetingId}/retry", { params: { path: { meetingId: id } } }));
}

export async function deleteMeeting(id: string) {
  const res = await api.DELETE("/v1/meetings/{meetingId}", { params: { path: { meetingId: id } } });
  if (!res.response.ok) throw new ApiError(res.error?.error || `Delete failed (${res.response.status})`, res.response.status);
}

export async function setActionItemDone(id: string, done: boolean) {
  return unwrap(await api.PATCH("/v1/action-items/{actionItemId}", { params: { path: { actionItemId: id } }, body: { done } }));
}

export async function searchMeetings(q: string) {
  return unwrap(await api.GET("/v1/search", { params: { query: { q, limit: 10 } } }));
}

export function recordingUrl(id: string) {
  return `/v1/meetings/${id}/recording`;
}
