import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { getMeeting } from "../api/client";
import { Icon, type IconName } from "../components/icons";
import { btn } from "../components/ui";
import { missingSummary } from "../lib/meetings";
import { Link } from "../router";

export function Highlights({ meetingId }: { meetingId?: string }) {
  const meeting = useQuery({ queryKey: ["meeting", meetingId], queryFn: () => getMeeting(meetingId!), enabled: !!meetingId });
  const m = meeting.data;

  return (
    <div className="grid gap-6 p-5">
      <div className="flex items-center gap-2 font-medium">
        <Icon name="sparkles" className="size-5 text-violet-300" />
        Highlights
      </div>

      {!meetingId ? (
        <p className="text-muted">Highlights from your latest meeting appear here once it's processed.</p>
      ) : !m ? (
        <p className="text-muted">{meeting.isError ? meeting.error.message : "Loading highlights…"}</p>
      ) : !m.summary ? (
        <>
          <LatestTitle title={m.title} />
          <p className="text-muted">{missingSummary(m)}</p>
          <OpenMeeting id={m.id} />
        </>
      ) : (
        <>
          <LatestTitle title={m.title} />
          <p className="leading-relaxed text-ink/90">{m.summary.overview}</p>
          <Group icon="checkCircle" title="Action items" empty="No action items.">
            {m.action_items.map((a) => (
              <li key={a.id} className={a.done ? "text-muted line-through" : ""}>
                {a.text}
                {(a.owner || a.due) && <span className="text-muted"> · {[a.owner, a.due && `due ${a.due}`].filter(Boolean).join(", ")}</span>}
              </li>
            ))}
          </Group>
          <Group icon="target" title="Decisions" empty="No decisions recorded.">
            {m.summary.decisions.map((d, i) => (
              <li key={i}>{d}</li>
            ))}
          </Group>
          <Group icon="help" title="Open questions" empty="No open questions.">
            {m.summary.questions.map((q, i) => (
              <li key={i}>{q}</li>
            ))}
          </Group>
          <OpenMeeting id={m.id} />
        </>
      )}
    </div>
  );
}

function LatestTitle({ title }: { title: string }) {
  return (
    <div>
      <p className="text-xs text-muted">Latest meeting</p>
      <h2 className="text-lg font-medium text-balance">{title}</h2>
    </div>
  );
}

function OpenMeeting({ id }: { id: string }) {
  return (
    <Link to={`/meetings/${id}`} className={`${btn.secondary} justify-self-start`}>
      Open meeting
      <Icon name="arrowRight" className="size-4" />
    </Link>
  );
}

function Group({ icon, title, empty, children }: { icon: IconName; title: string; empty: string; children: ReactNode[] }) {
  return (
    <section className="grid gap-2">
      <h3 className="inline-flex items-center gap-2 justify-self-start rounded-lg bg-raised px-3 py-1.5 font-medium">
        <Icon name={icon} className="size-4 text-violet-300" />
        {title}
      </h3>
      {children.length ? <ul className="grid list-disc gap-1 pl-8 text-ink/85">{children}</ul> : <p className="pl-1 text-muted">{empty}</p>}
    </section>
  );
}
