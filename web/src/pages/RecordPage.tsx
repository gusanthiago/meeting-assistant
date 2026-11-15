import { useEffect, useMemo, useRef, useState } from "react";
import { createMeeting, uploadRecording } from "../api/client";
import { Icon } from "../components/icons";
import { Page, TopActions } from "../components/layout";
import { btn, card, errorBox, Eyebrow, input, roundBtn } from "../components/ui";
import { formatClock } from "../lib/format";
import { navigate } from "../router";

type RecState = "idle" | "recording" | "paused" | "stopped";

const STATE_LABEL: Record<RecState, string> = {
  idle: "Ready to record",
  recording: "Recording",
  paused: "Paused",
  stopped: "Recording finished",
};

function preferredMime() {
  const options = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus", "audio/mp4"];
  return options.find((t) => typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported(t)) ?? "";
}

function audioDuration(file: Blob): Promise<number | undefined> {
  return new Promise((resolve) => {
    const el = document.createElement("audio");
    const url = URL.createObjectURL(file);
    const done = (ms?: number) => {
      URL.revokeObjectURL(url);
      resolve(ms);
    };
    el.preload = "metadata";
    el.onloadedmetadata = () => done(Number.isFinite(el.duration) ? el.duration * 1000 : undefined);
    el.onerror = () => done();
    el.src = url;
  });
}

export function RecordPage() {
  const [title, setTitle] = useState("");
  const [participants, setParticipants] = useState("");
  const [state, setState] = useState<RecState>("idle");
  const [elapsed, setElapsed] = useState(0);
  const [audio, setAudio] = useState<{ blob: Blob; durationMs?: number; name: string } | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const recorder = useRef<MediaRecorder | null>(null);
  const chunks = useRef<Blob[]>([]);
  const clock = useRef({ base: 0, since: 0 });
  const idempotencyKey = useRef(crypto.randomUUID());

  useEffect(() => {
    if (state !== "recording") return;
    const t = setInterval(() => setElapsed(clock.current.base + Date.now() - clock.current.since), 250);
    return () => clearInterval(t);
  }, [state]);

  useEffect(() => () => recorder.current?.stream.getTracks().forEach((t) => t.stop()), []);

  const previewUrl = useMemo(() => (audio ? URL.createObjectURL(audio.blob) : ""), [audio]);
  useEffect(
    () => () => {
      if (previewUrl) URL.revokeObjectURL(previewUrl);
    },
    [previewUrl],
  );

  async function start() {
    setError("");
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const mime = preferredMime();
      const rec = new MediaRecorder(stream, mime ? { mimeType: mime } : undefined);
      chunks.current = [];
      rec.ondataavailable = (e) => e.data.size > 0 && chunks.current.push(e.data);
      rec.onstop = () => {
        stream.getTracks().forEach((t) => t.stop());
        setAudio({ blob: new Blob(chunks.current, { type: rec.mimeType || "audio/webm" }), durationMs: clock.current.base, name: "Microphone recording" });
      };
      rec.start(1000);
      recorder.current = rec;
      clock.current = { base: 0, since: Date.now() };
      setElapsed(0);
      setAudio(null);
      setState("recording");
    } catch {
      setError("The browser couldn't use the microphone. Allow microphone access for this site, or upload a file instead.");
    }
  }

  function pause() {
    recorder.current?.pause();
    clock.current.base += Date.now() - clock.current.since;
    setElapsed(clock.current.base);
    setState("paused");
  }

  function resume() {
    recorder.current?.resume();
    clock.current.since = Date.now();
    setState("recording");
  }

  function stop() {
    if (state === "recording") clock.current.base += Date.now() - clock.current.since;
    setElapsed(clock.current.base);
    recorder.current?.stop();
    setState("stopped");
  }

  async function pickFile(file?: File) {
    if (!file) return;
    setError("");
    setAudio({ blob: file, durationMs: await audioDuration(file), name: file.name });
  }

  async function submit() {
    if (!audio) return;
    setBusy(true);
    setError("");
    try {
      const names = participants.split(",").map((p) => p.trim()).filter(Boolean);
      const meeting = await createMeeting(title.trim() || "Untitled meeting", names, idempotencyKey.current);
      await uploadRecording(meeting.id, audio.blob, audio.durationMs);
      navigate(`/meetings/${meeting.id}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Upload failed.");
      setBusy(false);
    }
  }

  const recording = state === "recording" || state === "paused";

  return (
    <Page title="New recording" actions={<TopActions showRecord={false} />}>
      <div className="mx-auto grid max-w-2xl gap-5 px-5 py-8">
        <div className="grid gap-3">
          <Eyebrow badge="Local">Transcribed on your machine with Whisper</Eyebrow>
          <h2 className="font-display text-3xl leading-tight font-bold">
            Record the <span className="text-gradient">conversation</span>
          </h2>
        </div>
        <section className={`${card} grid gap-4 p-5`}>
          <label className="grid gap-1.5 text-muted">
            Title
            <input id="title" className={input} value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Weekly sync" maxLength={200} />
          </label>
          <label className="grid gap-1.5 text-muted">
            Participants
            <input id="participants" className={input} value={participants} onChange={(e) => setParticipants(e.target.value)} placeholder="Ana, Bruno, Carla" />
          </label>
        </section>

        <section className={`${card} grid justify-items-center gap-5 px-5 py-10 text-center`}>
          <div className={`inline-flex items-center gap-2 rounded-md px-2.5 py-1 text-xs font-medium ${state === "recording" ? "bg-red-500/15 text-red-300" : "bg-raised text-muted"}`} aria-live="polite">
            {state === "recording" && <span className="size-1.5 animate-pulse rounded-full bg-current motion-reduce:animate-none" />}
            {STATE_LABEL[state]}
          </div>
          <div className="text-6xl font-light tabular-nums tracking-tight">{formatClock(elapsed)}</div>
          <div className="flex items-center gap-4">
            {!recording && (
              <button type="button" className={roundBtn.primary} onClick={start} aria-label={state === "stopped" ? "Record again" : "Start recording"}>
                <Icon name="mic" className="size-6" />
              </button>
            )}
            {state === "recording" && (
              <button type="button" className={roundBtn.secondary} onClick={pause} aria-label="Pause">
                <Icon name="pause" className="size-6" />
              </button>
            )}
            {state === "paused" && (
              <button type="button" className={roundBtn.primary} onClick={resume} aria-label="Resume">
                <Icon name="mic" className="size-6" />
              </button>
            )}
            {recording && (
              <button type="button" className={roundBtn.danger} onClick={stop} aria-label="Stop">
                <Icon name="stop" className="size-6" />
              </button>
            )}
          </div>
          {!recording && (
            <label className={`${btn.secondary} relative`}>
              <Icon name="upload" className="size-4" />
              Upload an audio file
              <input id="file" type="file" accept="audio/*" className="absolute inset-0 cursor-pointer opacity-0" onChange={(e) => pickFile(e.target.files?.[0])} />
            </label>
          )}
        </section>

        {audio && !recording && (
          <section className={`${card} grid gap-4 p-5`}>
            <div className="flex flex-wrap items-center justify-between gap-2 text-muted">
              <span className="truncate">{audio.name}</span>
              <span className="tabular-nums">
                {(audio.blob.size / 1024 / 1024).toFixed(1)} MB{audio.durationMs ? ` · ${formatClock(audio.durationMs)}` : ""}
              </span>
            </div>
            <audio controls src={previewUrl} className="w-full" />
            <button type="button" className={`${btn.primary} justify-self-end`} onClick={submit} disabled={busy}>
              <Icon name="sparkles" className="size-4" />
              {busy ? "Saving…" : "Save and process"}
            </button>
          </section>
        )}

        {error && <p className={errorBox}>{error}</p>}
      </div>
    </Page>
  );
}
