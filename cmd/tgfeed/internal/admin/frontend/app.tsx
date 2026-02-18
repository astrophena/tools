import React, { useCallback, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";

import { getText, putText, toAPIError } from "./api.ts";
import { EditorPanel } from "./components/EditorPanel.tsx";
import { StatsView } from "./components/StatsView.tsx";
import { useEditableResource } from "./hooks/useEditableResource.ts";
import { StatsRun } from "./types.ts";

/** Available primary dashboard tabs. */
type RouteTab = "stats" | "configuration";

/**
 * Resolves the active tab from the browser pathname.
 */
function routeFromPathname(pathname: string): RouteTab {
  if (pathname === "/config" || pathname === "/configuration") {
    return "configuration";
  }
  return "stats";
}

/**
 * Converts a tab into a canonical URL path.
 */
function pathnameForRoute(route: RouteTab): string {
  if (route === "configuration") {
    return "/config";
  }
  return "/stats";
}

const rootContainer = document.getElementById("root");
const dashboardLogo = rootContainer?.dataset.logo ?? "/static/icons/logo.webp";

/**
 * Main tgfeed admin dashboard root component.
 */
function App() {
  const [route, setRoute] = useState<RouteTab>(() =>
    routeFromPathname(window.location.pathname)
  );

  const config = useEditableResource({
    load: async () => await getText("/api/config"),
    save: async (value) => {
      await putText("/api/config", value, "text/plain; charset=utf-8");
    },
  });

  const errorTemplate = useEditableResource({
    load: async () => await getText("/api/error-template"),
    save: async (value) => {
      await putText("/api/error-template", value, "text/plain; charset=utf-8");
    },
  });

  const [stats, setStats] = useState<StatsRun[]>([]);
  const [statsLoading, setStatsLoading] = useState(false);
  const [statsError, setStatsError] = useState("");
  const [selectedStatsIndex, setSelectedStatsIndex] = useState(0);
  const [lastStatsRefreshedAt, setLastStatsRefreshedAt] = useState<
    number | null
  >(null);
  const [autoRefreshStats, setAutoRefreshStats] = useState(false);
  const [banner, setBanner] = useState("");

  /** Loads persisted run stats used by dashboard charts and indicators. */
  const loadStats = useCallback(async () => {
    setStatsLoading(true);
    setStatsError("");
    try {
      const response = await fetch("/api/stats", {
        method: "GET",
        headers: { Accept: "application/json" },
      });
      if (response.status === 404) {
        setStats([]);
        setLastStatsRefreshedAt(Date.now());
        return;
      }
      if (!response.ok) {
        throw await toAPIError(response);
      }
      const payload: unknown = await response.json();
      if (Array.isArray(payload)) {
        const newStats = payload as StatsRun[];
        if (
          stats.length > 0 &&
          newStats.length === stats.length &&
          newStats[0]?.start_time === stats[0]?.start_time
        ) {
          setLastStatsRefreshedAt(Date.now());
          return;
        }
        setStats(newStats);
      } else {
        setStats([]);
      }
      setLastStatsRefreshedAt(Date.now());
    } catch (err) {
      setStatsError(err instanceof Error ? err.message : "Unexpected error");
      setStats([]);
    } finally {
      setStatsLoading(false);
    }
  }, [stats]);

  /** Refreshes all editable resources and stats in one action. */
  async function refreshAll(): Promise<void> {
    setBanner("");
    await Promise.all([config.load(), errorTemplate.load(), loadStats()]);
  }

  /** Persists all dirty resources and shows a summary banner. */
  async function saveAll(): Promise<void> {
    const jobs: Array<Promise<boolean>> = [];
    if (config.dirty) {
      jobs.push(config.save());
    }
    if (errorTemplate.dirty) {
      jobs.push(errorTemplate.save());
    }
    if (jobs.length === 0) {
      setBanner("Nothing to save");
      return;
    }
    const results = await Promise.all(jobs);
    if (results.every(Boolean)) {
      setBanner("All changes saved");
    } else {
      setBanner("Some changes failed to save");
    }
  }

  useEffect(() => {
    void Promise.all([config.load(), errorTemplate.load(), loadStats()]);
  }, []);

  useEffect(() => {
    setSelectedStatsIndex(0);
  }, [stats]);

  useEffect(() => {
    if (!autoRefreshStats || route !== "stats") {
      return;
    }
    const timer = window.setInterval(() => {
      void loadStats();
    }, 30_000);
    return () => {
      window.clearInterval(timer);
    };
  }, [autoRefreshStats, loadStats, route]);

  useEffect(() => {
    function onPopState(): void {
      setRoute(routeFromPathname(window.location.pathname));
    }
    window.addEventListener("popstate", onPopState);
    return () => {
      window.removeEventListener("popstate", onPopState);
    };
  }, []);

  /** Navigates between stats and configuration tabs using pathname URLs. */
  function navigate(next: RouteTab): void {
    const nextPath = pathnameForRoute(next);
    if (window.location.pathname !== nextPath) {
      window.history.pushState({}, "", nextPath);
    }
    setRoute(next);
  }

  return (
    <div className="app-shell">
      <header className="panel hero">
        <div className="hero-title">
          <div className="hero-brand">
            <img className="hero-logo" src={dashboardLogo} alt="tgfeed logo" />
            <div>
              <p className="eyebrow">tgfeed</p>
              <h1>Admin Dashboard</h1>
            </div>
          </div>
          <p className="subtitle">
            Inspect run metrics and edit tgfeed configuration from one place.
          </p>
        </div>
        <div className="hero-actions">
          <button
            className="button button-ghost"
            type="button"
            onClick={() => void refreshAll()}
          >
            Refresh all
          </button>
          <button
            className="button button-solid"
            type="button"
            onClick={() => void saveAll()}
          >
            Save all
          </button>
        </div>
      </header>

      <nav className="tab-nav" aria-label="Dashboard sections">
        <button
          type="button"
          className={route === "stats" ? "tab-button active" : "tab-button"}
          onClick={() => navigate("stats")}
        >
          Stats
        </button>
        <button
          type="button"
          className={route === "configuration"
            ? "tab-button active"
            : "tab-button"}
          onClick={() => navigate("configuration")}
        >
          Configuration
        </button>
      </nav>

      {banner && <p className="message message-banner">{banner}</p>}

      <main className="dashboard-grid">
        {route === "stats" && (
          <StatsView
            stats={stats}
            statsLoading={statsLoading}
            statsError={statsError}
            selectedStatsIndex={selectedStatsIndex}
            setSelectedStatsIndex={setSelectedStatsIndex}
            loadStats={loadStats}
            lastStatsRefreshedAt={lastStatsRefreshedAt}
            autoRefreshStats={autoRefreshStats}
            setAutoRefreshStats={setAutoRefreshStats}
          />
        )}

        {route === "configuration" && (
          <div className="column">
            <EditorPanel
              title="Config"
              description="Starlark feed definitions and filters."
              placeholder='feed(url = "https://example.com/rss.xml")'
              languageHint="starlark"
              resource={config}
            />
            <EditorPanel
              title="Error Template"
              description="Template used for posting error notifications."
              placeholder="Fetch failed: %v"
              languageHint="template"
              resource={errorTemplate}
            />
          </div>
        )}
      </main>
    </div>
  );
}

if (rootContainer) {
  const root = createRoot(rootContainer);
  root.render(<App />);
}
