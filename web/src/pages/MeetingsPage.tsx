import { useQuery } from "@tanstack/react-query";
import { useEffect, useState, type FormEvent } from "react";
import { searchMeetings, type Meeting } from "../api/client";
import { Icon } from "../components/icons";
import { Page, TopActions } from "../components/layout";
import { Avatar, btn, card, errorBox, Eyebrow, SnippetText, StatusBadge, tag, timestampClass } from "../components/ui";
import { useDebounced, useMeetings } from "../hooks";
import { formatClock, formatDuration } from "../lib/format";
import { FILTERS, groupByDay, matchesFilter, meetingMeta, parseFilter } from "../lib/meetings";
import { Link, navigate, useLocation } from "../router";
import { Highlights } from "./Highlights";

export function MeetingsPage() {
  const { query } = useLocation();
  const q = useDebounced((query.get("q") ?? "").trim(), 250);
  const filter = parseFilter(query.get("status"));
  const meetings = useMeetings();
  const visible = meetings.items.filter((m) => matchesFilter(filter, m));
  const failed = meetings.items.filter((m) => m.status === "failed").length;
  const latestReady = meetings.items.find((m) => m.status === "ready");

  return (
    <Page title="Meetings" actions={<TopActions />} aside={<Highlights meetingId={latestReady?.id} />}>
      <div className="mx-auto grid max-w-4xl gap-5 px-5 py-5">
        <Hero items={meetings.items} />
        <div className="flex flex-wrap items-center gap-3">
          <div className="inline-flex rounded-lg border border-line p-0.5" role="tablist" aria-label="Filter meetings">
            {FILTERS.map((f) => (
              <Link
                key={f.value}
                to={f.value === "all" ? "/" : `/?status=${f.value}`}
                role="tab"
                aria-selected={!q && filter === f.value}
                className={`rounded-md px-3 py-1.5 no-underline transition ${!q && filter === f.value ? "bg-raised text-ink" : "text-muted hover:text-ink"}`}
              >
                {f.value === "all" ? "All" : f.label}
              </Link>
            ))}
          </div>
        </div>

        {failed > 0 && filter !== "failed" && !q && (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-2.5 text-amber-100">
            <span className="inline-flex items-center gap-2">
              <Icon name="alert" className="size-4 text-amber-300" />
              {failed === 1 ? "One of your recent meetings couldn't be processed." : `${failed} recent meetings couldn't be processed.`}
            </span>
            <Link to="/?status=failed" className="inline-flex items-center gap-1 font-medium text-violet-300 no-underline hover:text-violet-200">
              Show failed
              <Icon name="arrowRight" className="size-4" />
            </Link>
          </div>
        )}

        {q ? <SearchResults q={q} /> : <MeetingList meetings={meetings} visible={visible} />}
      </div>
    </Page>
  );
}

function MeetingList({ meetings, visible }: { meetings: ReturnType<typeof useMeetings>; visible: Meeting[] }) {
  if (meetings.isPending) return <p className="text-muted">Loading meetings…</p>;
  if (meetings.isError) return <p className={errorBox}>{meetings.error.message}</p>;
  if (meetings.items.length === 0) {
    return (
      <div className="grid justify-items-center gap-3 rounded-xl border border-dashed border-line px-4 py-16 text-center">
        <Icon name="mic" className="size-6 text-muted" />
        <p>No meetings yet.</p>
        <Link to="/record" className={btn.primary}>
          Record your first meeting
        </Link>
      </div>
    );
  }
  return (
    <>
      {visible.length === 0 && <p className="text-muted">No meetings match this filter.</p>}
      {groupByDay(visible).map((group) => (
        <section key={group.key} className="grid gap-2">
          <h2 className="px-1 text-muted">{group.label}</h2>
          {group.items.map((m) => (
            <MeetingCard key={m.id} meeting={m} />
          ))}
        </section>
      ))}
      <div className="py-2 text-center text-xs text-muted">
        {meetings.hasNextPage ? (
          <button type="button" className={btn.secondary} onClick={() => meetings.fetchNextPage()} disabled={meetings.isFetchingNextPage}>
            Load more
          </button>
        ) : (
          "You've reached the end of your meetings."
        )}
      </div>
    </>
  );
}

function MeetingCard({ meeting: m }: { meeting: Meeting }) {
  return (
    <Link to={`/meetings/${m.id}`} className={`${card} group flex items-center gap-4 px-4 py-3.5 text-ink no-underline transition hover:border-accent/50 hover:bg-raised`}>
      <Avatar name={m.title} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1 text-[15px] font-medium">
          <span className="truncate">{m.title}</span>
          <Icon name="chevronRight" className="size-4 shrink-0 text-muted transition group-hover:translate-x-0.5" />
        </div>
        <div className="truncate text-muted">{meetingMeta(m)}</div>
      </div>
      {m.status !== "ready" && <StatusBadge status={m.status} />}
    </Link>
  );
}

function SearchResults({ q }: { q: string }) {
  const results = useQuery({ queryKey: ["search", q], queryFn: () => searchMeetings(q) });
  if (results.isPending) return <p className="text-muted">Searching…</p>;
  if (results.isError) return <p className={errorBox}>{results.error.message}</p>;
  return (
    <section className="grid gap-2" aria-live="polite">
      <h2 className="px-1 text-muted">
        {results.data.items.length ? `Results for “${q}”` : `No meetings mention “${q}”.`}
      </h2>
      {results.data.items.map((hit) => (
        <article key={hit.meeting_id} className={`${card} px-4 py-3.5`}>
          <Link to={`/meetings/${hit.meeting_id}`} className="flex items-center gap-3 text-[15px] font-medium text-ink no-underline hover:text-violet-200">
            <Avatar name={hit.title} size="sm" />
            {hit.title}
          </Link>
          <ul className="mt-2 grid gap-1.5">
            {hit.snippets.map((s, i) => (
              <li key={i} className="grid grid-cols-[64px_1fr] items-start gap-2 text-ink/85">
                {s.start_ms != null ? (
                  <Link to={`/meetings/${hit.meeting_id}?t=${s.start_ms}`} className={timestampClass}>
                    {formatClock(s.start_ms)}
                  </Link>
                ) : (
                  <span className="pt-0.5 font-mono text-[11px] uppercase text-muted">{s.field.replace("_", " ")}</span>
                )}
                <span className="min-w-0">
                  {s.speaker && <strong className="text-violet-200">{s.speaker}: </strong>}
                  <SnippetText parts={s.parts} />
                </span>
              </li>
            ))}
          </ul>
        </article>
      ))}
    </section>
  );
}

const FEATURES = ["Local transcription", "Summaries", "Action items", "Search everything"];

function Hero({ items }: { items: Meeting[] }) {
  const { query } = useLocation();
  const urlQ = query.get("q") ?? "";
  const [q, setQ] = useState(urlQ);
  useEffect(() => setQ(urlQ), [urlQ]);

  function search(value: string) {
    setQ(value);
    navigate(value.trim() ? `/?q=${encodeURIComponent(value)}` : "/", { replace: true });
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    search(q);
  }

  const recorded = items.reduce((sum, m) => sum + (m.duration_ms ?? 0), 0);
  const stats = [
    { value: String(items.length), label: items.length === 1 ? "meeting" : "meetings" },
    { value: items.length ? formatDuration(recorded) : "0 s", label: "recorded" },
    { value: String(items.filter((m) => m.status === "ready").length), label: "ready to read" },
  ];

  return (
    <section className="relative overflow-hidden rounded-2xl border border-line bg-surface/70 p-6 sm:p-8">
      <div aria-hidden="true" className="pointer-events-none absolute -top-28 -left-20 size-80 rounded-full bg-accent/25 blur-3xl" />
      <div className="relative grid gap-6 lg:grid-cols-[1fr_auto] lg:items-center">
        <div className="grid gap-4">
          <Eyebrow badge="AI-powered">Notes that write themselves</Eyebrow>
          <h2 className="font-display text-3xl leading-tight font-bold text-balance sm:text-4xl">
            Turn every meeting into <span className="text-gradient">clear next steps</span>
          </h2>
          <p className="max-w-xl text-muted">Record or upload a meeting and get a transcript, a summary and action items, all searchable.</p>
          <form onSubmit={submit} role="search" className="flex max-w-xl items-center gap-2 rounded-full border border-line bg-panel p-1.5 pl-5 transition focus-within:border-accent">
            <label htmlFor="hero-search" className="sr-only">
              Search meetings
            </label>
            <input id="hero-search" type="search" value={q} onChange={(e) => search(e.target.value)} placeholder="Search a topic, a decision or a name" className="min-w-0 flex-1 bg-transparent outline-none" />
            <button type="submit" className={btn.primary}>
              <Icon name="sparkles" className="size-4" />
              Search
            </button>
          </form>
        </div>
        <ul className="flex flex-wrap gap-2 lg:grid lg:justify-items-start" aria-label="Features">
          {FEATURES.map((f) => (
            <li key={f} className={tag}>
              {f}
            </li>
          ))}
        </ul>
      </div>
      <dl className="relative mt-6 grid grid-cols-3 gap-3">
        {stats.map((s) => (
          <div key={s.label} className="rounded-xl border border-line bg-panel/80 px-4 py-3">
            <dt className="sr-only">{s.label}</dt>
            <dd className="font-display text-2xl font-bold tabular-nums text-gradient">{s.value}</dd>
            <dd className="text-xs text-muted" aria-hidden="true">
              {s.label}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
