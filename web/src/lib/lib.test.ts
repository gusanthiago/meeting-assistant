import type { MeetingStatus } from "../api/client";
import { formatClock, formatDuration } from "./format";
import { hasTranscript, noSpeech, pollInterval, statusTone } from "./status";

describe("formatClock", () => {
  it.each([
    [0, "0:00"],
    [9_400, "0:09"],
    [75_000, "1:15"],
    [3_725_000, "1:02:05"],
    [-5, "0:00"],
  ])("formats %d ms as %s", (ms, want) => {
    expect(formatClock(ms)).toBe(want);
  });
});

describe("formatDuration", () => {
  it.each([
    [null, "–"],
    [undefined, "–"],
    [400, "1 s"],
    [45_000, "45 s"],
    [30 * 60_000, "30 min"],
    [65 * 60_000, "1 h 05 min"],
  ])("formats %s as %s", (ms, want) => {
    expect(formatDuration(ms)).toBe(want);
  });
});

describe.each<[MeetingStatus, string, number | false, boolean]>([
  ["recording", "neutral", false, false],
  ["queued", "progress", 2000, false],
  ["transcribing", "progress", 2000, false],
  ["summarizing", "progress", 2000, true],
  ["ready", "good", false, true],
  ["failed", "bad", false, true],
])("status %s", (status, tone, interval, transcript) => {
  it(`has tone ${tone}`, () => expect(statusTone(status)).toBe(tone));
  it(`polls every ${interval}`, () => expect(pollInterval(status)).toBe(interval));
  it(`transcript available: ${transcript}`, () => expect(hasTranscript(status)).toBe(transcript));
});

describe("noSpeech", () => {
  it.each<[MeetingStatus | undefined, unknown[] | undefined, boolean]>([
    ["ready", [], true],
    ["ready", [{ text: "Hi" }], false],
    ["ready", undefined, false],
    ["summarizing", [], false],
    ["failed", [], false],
    [undefined, [], false],
  ])("status %s with %j segments → %s", (status, segments, want) => {
    expect(noSpeech(status, segments)).toBe(want);
  });
});
