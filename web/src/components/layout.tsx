import { useQuery } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { useMeetings } from "../hooks";
import { FILTERS, matchesFilter, parseFilter } from "../lib/meetings";
import { Link, navigate, useLocation } from "../router";
import { Icon, type IconName } from "./icons";
import { Avatar, btn, sectionTitle } from "./ui";

const DOCS_URL = import.meta.env.VITE_API_DOCS_URL ?? "http://localhost:8081";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-dvh overflow-hidden">
      <IconRail />
      <Sidebar />
      {children}
    </div>
  );
}

function IconRail() {
  const { path } = useLocation();
  const items: { to: string; icon: IconName; label: string; active: boolean }[] = [
    { to: "/", icon: "video", label: "Meetings", active: path === "/" || path.startsWith("/meetings/") },
    { to: "/record", icon: "mic", label: "Record", active: path === "/record" },
  ];
  const item = "inline-flex size-10 items-center justify-center rounded-lg transition";
  return (
    <nav className="flex w-16 shrink-0 flex-col items-center gap-1.5 border-r border-line bg-app py-4" aria-label="Main">
      <Link to="/" className="mb-5 inline-flex size-9 items-center justify-center rounded-lg bg-accent" aria-label="Meeting Assistant">
        <span className="inline-flex h-4 items-center gap-0.5" aria-hidden="true">
          {[7, 14, 10, 5].map((h) => (
            <i key={h} className="w-[2.5px] rounded-full bg-white" style={{ height: h }} />
          ))}
        </span>
      </Link>
      {items.map((i) => (
        <Link key={i.to} to={i.to} title={i.label} aria-label={i.label} aria-current={i.active ? "page" : undefined} className={`${item} ${i.active ? "bg-raised text-ink" : "text-muted hover:bg-raised hover:text-ink"}`}>
          <Icon name={i.icon} />
        </Link>
      ))}
      <a href={DOCS_URL} target="_blank" rel="noopener" title="API docs" aria-label="API docs" className={`${item} mt-auto text-muted hover:bg-raised hover:text-ink`}>
        <Icon name="book" />
      </a>
    </nav>
  );
}

const filterIcons: Record<string, IconName> = { all: "list", ready: "checkCircle", processing: "loader", failed: "alert" };

function Sidebar() {
  const { path, query } = useLocation();
  const urlQ = query.get("q") ?? "";
  const [q, setQ] = useState(urlQ);
  useEffect(() => setQ(urlQ), [urlQ]);
  const filter = parseFilter(query.get("status"));
  const { items } = useMeetings();
  const onList = path === "/";

  function search(value: string) {
    setQ(value);
    navigate(value.trim() ? `/?q=${encodeURIComponent(value)}` : "/", { replace: onList });
  }

  return (
    <aside className="hidden w-72 shrink-0 flex-col border-r border-line bg-panel lg:flex">
      <div className="border-b border-line p-3">
        <label className="relative block">
          <span className="sr-only">Search meetings</span>
          <Icon name="search" className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
          <input id="sidebar-search" type="search" value={q} onChange={(e) => search(e.target.value)} placeholder="Search meetings" className="w-full rounded-lg border border-line bg-surface py-2 pr-3 pl-9 outline-none transition focus:border-accent" />
        </label>
      </div>
      <nav className="grid gap-0.5 p-3" aria-label="Filters">
        {FILTERS.map((f) => {
          const active = onList && !query.get("q") && filter === f.value;
          const count = items.filter((m) => matchesFilter(f.value, m)).length;
          return (
            <Link
              key={f.value}
              to={f.value === "all" ? "/" : `/?status=${f.value}`}
              className={`flex items-center gap-3 rounded-lg px-3 py-2 no-underline transition ${active ? "bg-accent-soft text-violet-200" : "text-ink/85 hover:bg-raised"}`}
            >
              <Icon name={filterIcons[f.value]} className="size-4" />
              <span className="flex-1">{f.label}</span>
              {count > 0 && <span className="text-xs tabular-nums text-muted">{count}</span>}
            </Link>
          );
        })}
      </nav>
      <div className="min-h-0 flex-1 overflow-y-auto border-t border-line p-3">
        <div className={`${sectionTitle} px-3 pb-2`}>Recent</div>
        {items.length === 0 ? (
          <div className="grid justify-items-center gap-3 px-3 py-6 text-center text-muted">
            <p>Your recordings will show up here.</p>
            <Link to="/record" className={btn.secondary}>
              <Icon name="mic" className="size-4" />
              Record
            </Link>
          </div>
        ) : (
          <ul className="grid gap-0.5">
            {items.slice(0, 8).map((m) => (
              <li key={m.id}>
                <Link to={`/meetings/${m.id}`} className={`flex items-center gap-2.5 rounded-lg px-3 py-1.5 no-underline transition ${path === `/meetings/${m.id}` ? "bg-raised text-ink" : "text-ink/80 hover:bg-raised"}`}>
                  <Avatar name={m.title} size="sm" />
                  <span className="truncate">{m.title}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}

export function Page({ title, actions, aside, children }: { title: ReactNode; actions?: ReactNode; aside?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-line px-5">
        <h1 className="min-w-0 truncate text-[15px] font-medium">{title}</h1>
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      </header>
      <div className="flex min-h-0 flex-1">
        <main className="ambient min-w-0 flex-1 overflow-y-auto">
          {children}
          {aside && <div className="border-t border-line xl:hidden">{aside}</div>}
        </main>
        {aside && <aside className="hidden w-[380px] shrink-0 overflow-y-auto border-l border-line bg-panel xl:block">{aside}</aside>}
      </div>
    </div>
  );
}

export function TopActions({ showRecord = true }: { showRecord?: boolean }) {
  return (
    <>
      <SystemStatus />
      {showRecord && (
        <Link to="/record" className={btn.primary}>
          <Icon name="mic" className="size-4" />
          Record
        </Link>
      )}
    </>
  );
}

function SystemStatus() {
  const ready = useQuery({
    queryKey: ["readyz"],
    queryFn: async () => (await fetch("/readyz")).ok,
    refetchInterval: 30_000,
  });
  const ok = ready.data === true;
  const label = ready.isPending ? "Checking services" : ok ? "All services ready" : "Some services unavailable";
  return (
    <span className="hidden items-center gap-2 rounded-lg border border-line px-3 py-1.5 text-xs text-muted md:inline-flex" title="Postgres, OpenSearch and storage">
      <span className={`size-2 rounded-full ${ready.isPending ? "bg-muted" : ok ? "bg-emerald-400" : "bg-amber-400"}`} />
      {label}
    </span>
  );
}
