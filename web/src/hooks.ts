import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { listMeetings } from "./api/client";
import { isProcessing } from "./lib/status";

// useMeetings is shared by the sidebar and the meetings page, so both read the same cached pages.
export function useMeetings() {
  const query = useInfiniteQuery({
    queryKey: ["meetings"],
    queryFn: ({ pageParam }) => listMeetings(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    refetchInterval: (q) => (q.state.data?.pages.some((p) => p.items.some((m) => isProcessing(m.status))) ? 3000 : false),
  });
  return { ...query, items: query.data?.pages.flatMap((p) => p.items) ?? [] };
}

export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}
