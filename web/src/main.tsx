import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AppShell, Page, TopActions } from "./components/layout";
import { MeetingPage } from "./pages/MeetingPage";
import { MeetingsPage } from "./pages/MeetingsPage";
import { RecordPage } from "./pages/RecordPage";
import { Link, useLocation } from "./router";
import "./styles.css";

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } });

function App() {
  const { path } = useLocation();
  const meetingId = path.match(/^\/meetings\/([0-9a-f-]{36})$/)?.[1];

  let page;
  if (path === "/") page = <MeetingsPage />;
  else if (path === "/record") page = <RecordPage />;
  else if (meetingId) page = <MeetingPage key={meetingId} id={meetingId} />;
  else
    page = (
      <Page title="Not found" actions={<TopActions />}>
        <div className="grid justify-items-center gap-2 px-5 py-16 text-center">
          <p>This page doesn't exist.</p>
          <Link to="/" className="text-violet-300">
            Go to all meetings
          </Link>
        </div>
      </Page>
    );

  return <AppShell>{page}</AppShell>;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
