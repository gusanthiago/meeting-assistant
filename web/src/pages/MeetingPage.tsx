import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { deleteMeeting, getMeeting, getTranscript, recordingUrl, regenerateSummary, retryMeeting, setActionItemDone, type MeetingDetail } from "../api/client";
import { Icon } from "../components/icons";
import { Page, TopActions } from "../components/layout";
import { Avatar, btn, card, errorBox, sectionTitle, StatusBadge, TranscriptList, warningBox } from "../components/ui";
import { meetingMeta, summaryByline } from "../lib/meetings";
import { hasTranscript, isProcessing, noSpeech, pollInterval } from "../lib/status";
import { navigate, useLocation } from "../router";

export function MeetingPage({ id }: { id: string }) {
  const { query } = useLocation();
  const audioRef = useRef<HTMLAudioElement>(null);
  const [now, setNow] = useState(-1);
  const startAt = Number(query.get("t") ?? NaN);

  const meeting = useQuery({
    queryKey: ["meeting", id],
    queryFn: () => getMeeting(id),
    refetchInterval: (q) => pollInterval(q.state.data?.status),
  });
  const status = meeting.data?.status;
  const transcript = useQuery({
    queryKey: ["transcript", id, status === "ready"],
    queryFn: () => getTranscript(id),
    enabled: hasTranscript(status),
  });
  const qc = useQueryClient();
  const retry = useMutation({
    mutationFn: () => retryMeeting(id),
    onSuccess: () => refreshMeeting(qc, id),
  });
  const remove = useMutation({
    mutationFn: () => deleteMeeting(id),
    onSuccess: () => {
      navigate("/", { replace: true });
      qc.removeQueries({ queryKey: ["meeting", id] });
      return qc.invalidateQueries({ queryKey: ["meetings"] });
    },
  });

  function confirmDelete() {
    if (window.confirm("Delete this meeting? Its recording, transcript and summary are removed for good.")) remove.mutate();
  }

  function seek(ms: number) {
    const el = audioRef.current;
    if (!el) return;
    el.currentTime = ms / 1000;
    void el.play().catch(() => {});
  }

  useEffect(() => {
    if (!Number.isFinite(startAt) || !audioRef.current) return;
    const el = audioRef.current;
    const go = () => (el.currentTime = startAt / 1000);
    if (el.readyState >= 1) go();
    else el.addEventListener("loadedmetadata", go, { once: true });
  }, [startAt, meeting.data?.has_recording]);

  if (meeting.isPending || meeting.isError) {
    return (
      <Page title="Meeting" actions={<TopActions />}>
        <div className="p-5">{meeting.isError ? <p className={errorBox}>{meeting.error.message}</p> : <p className="text-muted">Loading meeting…</p>}</div>
      </Page>
    );
  }
  const m = meeting.data;
  const silent = noSpeech(m.status, transcript.data?.segments);

  return (
    <Page title={m.title} actions={<TopActions />} aside={<SummaryPanel meeting={m} silent={silent} />}>
      <div className="mx-auto grid max-w-4xl gap-5 px-5 py-5">
        <div className="flex items-center gap-4">
          <Avatar name={m.title} size="lg" />
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-xl font-semibold">{m.title}</h2>
            <p className="truncate text-muted">{meetingMeta(m)}</p>
          </div>
          <StatusBadge status={m.status} />
          <button type="button" className={`${btn.ghost} px-2.5 py-1.5`} onClick={confirmDelete} disabled={remove.isPending} title="Delete meeting" aria-label="Delete meeting">
            <Icon name="trash" className="size-4" />
          </button>
        </div>

        {remove.isError && <p className={errorBox}>{remove.error.message}</p>}
        {m.status === "failed" && (
          <div className={`${errorBox} flex flex-wrap items-center justify-between gap-3`}>
            <span>Processing failed: {m.error}</span>
            {m.has_recording && (
              <button type="button" className={btn.secondary} onClick={() => retry.mutate()} disabled={retry.isPending}>
                <Icon name="refresh" className="size-4" />
                Try again
              </button>
            )}
          </div>
        )}
        {retry.isError && <p className={errorBox}>{retry.error.message}</p>}
        {silent && (
          <div className={warningBox} role="status">
            <Icon name="alert" className="mt-0.5 size-4 shrink-0 text-amber-300" />
            <span>No speech was recognized in this recording, so there is no transcript or summary. Check that the right microphone is selected and record again.</span>
          </div>
        )}
        {isProcessing(m.status) && <p className="text-muted">This page updates by itself while the meeting is processed.</p>}

        {m.has_recording && (
          <div className={`${card} p-2`}>
            <audio ref={audioRef} controls preload="metadata" src={recordingUrl(id)} onTimeUpdate={(e) => setNow(e.currentTarget.currentTime * 1000)} className="w-full" />
          </div>
        )}

        <section className={`${card} p-2`}>
          <div className="flex items-center justify-between px-3 pt-2 pb-1">
            <h3 className="font-medium">Transcript</h3>
            {transcript.data && <span className="text-xs text-muted">{transcript.data.segments.length} lines</span>}
          </div>
          {silent ? (
            <p className="px-3 py-4 text-muted">No speech was recognized.</p>
          ) : transcript.data ? (
            <TranscriptList segments={transcript.data.segments} activeMs={now} onSeek={seek} />
          ) : (
            <p className="px-3 py-4 text-muted">{hasTranscript(m.status) ? "Loading transcript…" : "The transcript appears once transcription finishes."}</p>
          )}
        </section>
      </div>
    </Page>
  );
}

// refreshMeeting reloads the meeting and the list, so the sidebar and the failed banner see the new status.
function refreshMeeting(qc: QueryClient, id: string) {
  return Promise.all([qc.invalidateQueries({ queryKey: ["meeting", id] }), qc.invalidateQueries({ queryKey: ["meetings"] })]);
}

function SummaryPanel({ meeting: m, silent }: { meeting: MeetingDetail; silent: boolean }) {
  const qc = useQueryClient();
  const regenerate = useMutation({
    mutationFn: () => regenerateSummary(m.id),
    onSuccess: () => refreshMeeting(qc, m.id),
  });
  const toggle = useMutation({
    mutationFn: ({ itemId, done }: { itemId: string; done: boolean }) => setActionItemDone(itemId, done),
    onMutate: ({ itemId, done }) => {
      qc.setQueryData<MeetingDetail>(["meeting", m.id], (old) => old && { ...old, action_items: old.action_items.map((a) => (a.id === itemId ? { ...a, done } : a)) });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["meeting", m.id] }),
  });
  const open = m.action_items.filter((a) => !a.done).length;

  return (
    <div className="grid gap-6 p-5">
      <section className="grid gap-3">
        <div className="flex items-center justify-between gap-2">
          <h3 className="inline-flex items-center gap-2 font-medium">
            <Icon name="sparkles" className="size-5 text-violet-300" />
            Summary
          </h3>
          <button
            type="button"
            className={`${btn.ghost} px-2.5 py-1.5`}
            onClick={() => regenerate.mutate()}
            disabled={silent || regenerate.isPending || m.status !== "ready"}
            title="Regenerate summary"
            aria-label="Regenerate summary"
          >
            <Icon name="refresh" className="size-4" />
          </button>
        </div>
        {m.status === "ready" && m.summary_error && (
          <div className={`${warningBox} flex-wrap justify-between`} role="status">
            <span className="inline-flex items-start gap-2">
              <Icon name="alert" className="mt-0.5 size-4 shrink-0 text-amber-300" />
              <span>
                A new summary couldn't be written: {m.summary_error}
                {m.summary && " The summary below is from an earlier run."}
              </span>
            </span>
            <button type="button" className={btn.secondary} onClick={() => regenerate.mutate()} disabled={regenerate.isPending}>
              <Icon name="refresh" className="size-4" />
              Try again
            </button>
          </div>
        )}
        {m.summary ? (
          <>
            <p className="leading-relaxed text-ink/90">{m.summary.overview}</p>
            <SummaryList title="Key takeaways" items={m.summary.takeaways} />
            <SummaryList title="Decisions" items={m.summary.decisions} />
            <SummaryList title="Open questions" items={m.summary.questions} />
            <p className="text-xs text-muted">{summaryByline(m.summary)}</p>
          </>
        ) : (
          !m.summary_error && <p className="text-muted">{silent ? "Nothing to summarize: no speech was recognized." : "The summary appears once the meeting is processed."}</p>
        )}
        {regenerate.isError && <p className={errorBox}>{regenerate.error.message}</p>}
      </section>

      <section className="grid gap-3">
        <div className="flex items-baseline justify-between">
          <h3 className="inline-flex items-center gap-2 font-medium">
            <Icon name="checkCircle" className="size-5 text-violet-300" />
            Action items
          </h3>
          {m.action_items.length > 0 && <span className="text-xs text-muted">{open} open</span>}
        </div>
        {m.action_items.length > 0 ? (
          <ul className="grid gap-1.5">
            {m.action_items.map((a) => (
              <li key={a.id} className="flex items-start gap-3 rounded-lg border border-line bg-surface px-3 py-2.5">
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={a.done}
                  aria-label={a.done ? `Mark "${a.text}" as open` : `Mark "${a.text}" as done`}
                  onClick={() => toggle.mutate({ itemId: a.id, done: !a.done })}
                  className={`mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-md border transition cursor-pointer ${a.done ? "border-accent bg-accent text-white" : "border-line text-transparent hover:border-accent"}`}
                >
                  <Icon name="check" className="size-3.5" />
                </button>
                <div className="min-w-0">
                  <div className={a.done ? "text-muted line-through" : ""}>{a.text}</div>
                  <div className="text-xs text-muted">
                    {a.owner || "No owner"}
                    {a.due && ` · due ${a.due}`}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-muted">{m.status === "ready" ? "No action items were found in this meeting." : "Action items appear once the meeting is processed."}</p>
        )}
      </section>
    </div>
  );
}

function SummaryList({ title, items }: { title: string; items: string[] }) {
  if (!items.length) return null;
  return (
    <section>
      <h4 className={`${sectionTitle} mb-1`}>{title}</h4>
      <ul className="grid list-disc gap-1 pl-5 text-ink/85">
        {items.map((t, i) => (
          <li key={i}>{t}</li>
        ))}
      </ul>
    </section>
  );
}
