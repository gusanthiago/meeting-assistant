import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { MeetingStatus, Segment } from "../api/client";
import { SnippetText, StatusBadge, TranscriptList } from "./ui";

afterEach(cleanup);

describe("StatusBadge", () => {
  it.each<[MeetingStatus, string, string]>([
    ["recording", "Waiting for audio", "neutral"],
    ["transcribing", "Transcribing", "progress"],
    ["ready", "Ready", "good"],
    ["failed", "Failed", "bad"],
  ])("shows %s as %s", (status, label, tone) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(label).closest("[data-tone]")?.getAttribute("data-tone")).toBe(tone);
  });
});

describe("SnippetText", () => {
  it.each([
    [[{ text: "plain", match: false }], 0],
    [[{ text: "send the ", match: false }, { text: "report", match: true }], 1],
    [[{ text: "a", match: true }, { text: " and ", match: false }, { text: "b", match: true }], 2],
  ])("marks %j with %d highlights", (parts, marks) => {
    const { container } = render(<SnippetText parts={parts} />);
    expect(container.querySelectorAll("mark")).toHaveLength(marks);
    expect(container.textContent).toBe(parts.map((p) => p.text).join(""));
  });
});

describe("TranscriptList", () => {
  const segments: Segment[] = [
    { speaker: "Ana", start_ms: 0, end_ms: 4_000, text: "Let's start." },
    { speaker: "Bruno", start_ms: 4_500, end_ms: 9_000, text: "I will send the notes." },
    { speaker: "Ana", start_ms: 65_000, end_ms: 70_000, text: "Thanks all." },
  ];

  it.each([
    ["0:00", 0],
    ["0:04", 4_500],
    ["1:05", 65_000],
  ])("seeks to %s", (label, ms) => {
    const onSeek = vi.fn();
    render(<TranscriptList segments={segments} activeMs={-1} onSeek={onSeek} />);
    fireEvent.click(screen.getByRole("button", { name: `Play from ${label}` }));
    expect(onSeek).toHaveBeenCalledWith(ms);
  });

  it.each([
    [1_000, "Let's start."],
    [5_000, "I will send the notes."],
    [66_000, "Thanks all."],
  ])("highlights the line playing at %d ms", (activeMs, text) => {
    render(<TranscriptList segments={segments} activeMs={activeMs} onSeek={() => {}} />);
    expect(screen.getByText(text).closest("li")?.getAttribute("data-active")).toBe("true");
  });

  it.each<[string, string[], number]>([
    ["different speakers", ["Ana", "Bruno", "Ana"], 3],
    ["one unnamed speaker", ["Speaker", "Speaker", "Speaker"], 1],
    ["runs of the same speaker", ["Ana", "Ana", "Bruno"], 2],
  ])("shows speaker names only when the speaker changes: %s", (_, speakers, labels) => {
    const segs = speakers.map((speaker, i) => ({ speaker, start_ms: i * 1000, end_ms: i * 1000 + 900, text: `line ${i}` }));
    const { container } = render(<TranscriptList segments={segs} activeMs={-1} onSeek={() => {}} />);
    expect(container.querySelectorAll(".font-semibold")).toHaveLength(labels);
  });
});
