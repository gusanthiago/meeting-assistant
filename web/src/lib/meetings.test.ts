import type { Meeting, MeetingStatus, Summary } from "../api/client";
import { groupByDay, initial, matchesFilter, meetingMeta, missingSummary, paletteIndex, parseFilter, summaryByline, type Filter } from "./meetings";

function meeting(over: Partial<Meeting>): Meeting {
  return {
    id: "1",
    title: "Weekly sync",
    participants: [],
    status: "ready",
    has_recording: true,
    created_at: "2026-11-12T12:00:00",
    updated_at: "2026-11-12T12:00:00",
    ...over,
  };
}

describe("parseFilter", () => {
  it.each<[string | null, Filter]>([
    [null, "all"],
    ["ready", "ready"],
    ["processing", "processing"],
    ["failed", "failed"],
    ["bogus", "all"],
  ])("parses %s as %s", (raw, want) => {
    expect(parseFilter(raw)).toBe(want);
  });
});

describe.each<[Filter, MeetingStatus[], MeetingStatus[]]>([
  ["all", ["recording", "queued", "ready", "failed"], []],
  ["ready", ["ready"], ["queued", "failed"]],
  ["processing", ["queued", "transcribing", "summarizing"], ["ready", "failed", "recording"]],
  ["failed", ["failed"], ["ready", "queued"]],
])("filter %s", (filter, included, excluded) => {
  it.each(included)("includes %s", (status) => expect(matchesFilter(filter, meeting({ status }))).toBe(true));
  it.each(excluded.length ? excluded : [null])("excludes %s", (status) => {
    if (status) expect(matchesFilter(filter, meeting({ status }))).toBe(false);
  });
});

describe("groupByDay", () => {
  it.each([
    ["one day", ["2026-11-12T09:00:00", "2026-11-12T15:00:00"], [2]],
    ["two days", ["2026-11-12T09:00:00", "2026-11-11T09:00:00", "2026-11-11T08:00:00"], [1, 2]],
    ["no meetings", [], []],
  ])("%s", (_, dates, sizes) => {
    const groups = groupByDay(dates.map((d, i) => meeting({ id: String(i), started_at: d })));
    expect(groups.map((g) => g.items.length)).toEqual(sizes);
  });
});

describe("avatars", () => {
  it.each([
    ["Weekly sync", "W"],
    ["  sprint planning", "S"],
    ["2026 kickoff", "2"],
    ["Ünits review", "Ü"],
    ["!!!", "?"],
  ])("initial of %j is %s", (name, want) => {
    expect(initial(name)).toBe(want);
  });

  it.each(["Weekly sync", "Sprint planning", "Customer call"])("color of %j is stable and in range", (name) => {
    expect(paletteIndex(name, 6)).toBe(paletteIndex(name, 6));
    expect(paletteIndex(name, 6)).toBeGreaterThanOrEqual(0);
    expect(paletteIndex(name, 6)).toBeLessThan(6);
  });
});

describe("meetingMeta", () => {
  it.each<[Partial<Meeting>, string[]]>([
    [{ duration_ms: 83 * 60_000, participants: ["Samuel"] }, ["1 h 23 min", "Samuel"]],
    [{ duration_ms: 5_000 }, ["5 s"]],
    [{ participants: ["Ana", "Bruno"] }, ["Ana, Bruno"]],
  ])("includes the right parts for %j", (over, parts) => {
    const meta = meetingMeta(meeting({ started_at: "2026-11-12T09:00:00", ...over }));
    for (const p of parts) expect(meta).toContain(p);
    expect(meta.split(" · ").length).toBe(2 + parts.length);
  });
});

describe("summaryByline", () => {
  const base: Summary = { overview: "", takeaways: [], decisions: [], questions: [], provider: "claude", model: "claude-haiku-5-5", created_at: "2026-11-15T10:00:00Z" };
  it.each<[string, Partial<Summary>, string]>([
    ["rule-based", { provider: "fake", model: null }, "Written by the rule-based summarizer"],
    ["claude with tokens", { input_tokens: 1813, output_tokens: 219 }, "Written by Claude (claude-haiku-5-5) · 1,813 in, 219 out tokens"],
    ["claude without tokens", {}, "Written by Claude (claude-haiku-5-5)"],
    ["claude without model", { model: null }, "Written by Claude (unknown model)"],
  ])("%s", (_, over, want) => {
    expect(summaryByline({ ...base, ...over })).toBe(want);
  });
});

describe("missingSummary", () => {
  it.each<[string | null | undefined, string]>([
    ["The Anthropic account has no credits.", "No summary yet: The Anthropic account has no credits."],
    [null, "No summary: no speech was recognized in this recording."],
    [undefined, "No summary: no speech was recognized in this recording."],
  ])("summary_error %j", (summaryError, want) => {
    expect(missingSummary({ summary_error: summaryError })).toBe(want);
  });
});
