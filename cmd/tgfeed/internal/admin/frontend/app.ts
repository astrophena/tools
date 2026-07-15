import htmx from "htmx.org";

import { registerProgressBar } from "./progress.ts";

declare global {
  interface Window {
    htmx: typeof htmx;
  }
}

(globalThis as typeof globalThis & { htmx: typeof htmx }).htmx = htmx;

function refreshPageMetadata(root: ParentNode = document): void {
  // Tab navigation swaps only dashboard-content to keep the surrounding shell
  // stable. Synchronize the shell state that the server would otherwise set
  // during a full-page navigation.
  const content = document.getElementById("dashboard-content");
  const title = content?.dataset.pageTitle;
  if (title) document.title = title;
  const route = content?.dataset.route;
  if (route) {
    document.querySelectorAll<HTMLElement>(".tab-nav .tab-button").forEach(
      (button) => {
        const path = button.getAttribute("hx-get");
        button.classList.toggle(
          "active",
          route === "stats" ? path === "/stats" : path === "/config",
        );
      },
    );
    const refresh = document.getElementById("refresh-all");
    refresh?.setAttribute("hx-get", route === "stats" ? "/stats" : "/config");
    refresh?.setAttribute("href", route === "stats" ? "/stats" : "/config");
    if (refresh) {
      refresh.textContent = route === "stats" ? "Refresh stats" : "Reload all";
    }
    const save = document.getElementById("save-all") as
      | HTMLButtonElement
      | null;
    if (save) save.hidden = route !== "configuration";
  }
  root.querySelectorAll<HTMLElement>("time[data-local-time]").forEach(
    (element) => {
      const date = new Date(element.getAttribute("datetime") ?? "");
      if (Number.isNaN(date.getTime())) return;
      element.textContent = new Intl.DateTimeFormat(undefined, {
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      }).format(date);
    },
  );
}

function hasUnsavedChanges(root: ParentNode = document): boolean {
  return Array.from(
    root.querySelectorAll<HTMLTextAreaElement>(
      "textarea[data-code-editor]",
    ),
  ).some((textarea) =>
    textarea.value !== (textarea.dataset.baseline ?? textarea.defaultValue)
  );
}

function discardedEditorScope(element: HTMLElement): ParentNode | null {
  if (
    element.matches("#refresh-all") ||
    element.matches(".tab-nav .tab-button")
  ) return document;
  if (element.matches(".panel-actions .button-ghost")) {
    return element.closest("[data-editor-panel]");
  }
  return null;
}

type HtmxConfirmDetail = {
  elt: HTMLElement;
  issueRequest: (skipConfirmation?: boolean) => void;
};

document.addEventListener("htmx:confirm", (event) => {
  const detail = (event as CustomEvent<HtmxConfirmDetail>).detail;
  if (!detail?.elt) return;
  const scope = discardedEditorScope(detail.elt);
  if (!scope || !hasUnsavedChanges(scope)) return;
  event.preventDefault();
  if (globalThis.confirm("Discard unsaved changes?")) {
    detail.issueRequest(true);
  }
});

globalThis.addEventListener("beforeunload", (event) => {
  if (!hasUnsavedChanges()) return;
  event.preventDefault();
  event.returnValue = "";
});

let currentHistoryURL = location.href;
let currentHistoryState = history.state;

function rememberHistoryEntry(): void {
  currentHistoryURL = location.href;
  currentHistoryState = history.state;
}

document.addEventListener("htmx:pushedIntoHistory", rememberHistoryEntry);
document.addEventListener("htmx:replacedInHistory", rememberHistoryEntry);
globalThis.addEventListener("popstate", (event) => {
  if (
    !hasUnsavedChanges() || globalThis.confirm("Discard unsaved changes?")
  ) {
    rememberHistoryEntry();
    return;
  }
  event.stopImmediatePropagation();
  history.pushState(currentHistoryState, "", currentHistoryURL);
}, { capture: true });

async function loadEnhancements(root: ParentNode = document): Promise<void> {
  // Charts and CodeMirror dominate the JavaScript payload. Load each feature
  // only when the server-rendered subtree contains a matching enhancement.
  const jobs: Promise<void>[] = [];
  if (root.querySelector("[data-chart-spec]")) {
    jobs.push(
      import("./charts.ts").then((module) => module.registerCharts(root)),
    );
  }
  if (root.querySelector("[data-code-editor]")) {
    jobs.push(
      import("./editor.ts").then((module) => module.registerEditors(root)),
    );
  }
  await Promise.all(jobs);
}

function preloadEditorForConfiguration(event: Event): void {
  // Start fetching CodeMirror on intent so it is usually available by the time
  // the configuration fragment arrives, without adding it to the stats page.
  const target = event.target as Element | null;
  if (!target?.closest('.tab-nav [hx-get="/config"]')) return;
  void import("./editor.ts");
}

function liveSwapRoot(event: Event): ParentNode {
  const target = (event as CustomEvent).detail?.target;
  if (!(target instanceof HTMLElement)) return document;
  if (target.isConnected) return target;
  // An outerHTML swap can report the element that was removed. Resolve its ID
  // back to the replacement before scanning for lazy enhancements.
  return document.getElementById(target.id) ?? document;
}

document.addEventListener("htmx:afterSwap", (event) => {
  const target = liveSwapRoot(event);
  refreshPageMetadata(target);
  void loadEnhancements(target);
});
document.addEventListener("htmx:historyRestore", () => {
  refreshPageMetadata();
  void loadEnhancements();
});
document.addEventListener("pointerover", preloadEditorForConfiguration);
document.addEventListener("pointerdown", preloadEditorForConfiguration);
document.addEventListener("focusin", preloadEditorForConfiguration);
document.addEventListener("DOMContentLoaded", () => {
  refreshPageMetadata();
  registerProgressBar(document.body);
  void loadEnhancements();
});

document.addEventListener("toggle", (event) => {
  const details = event.target as HTMLDetailsElement;
  if (!details.matches("details[data-stats-details]")) return;
  // Details is ordinary HTML state, but keeping it in the URL lets refreshes,
  // history restoration, and subsequent HTMX requests preserve the choice.
  const url = new URL(location.href);
  if (details.open) url.searchParams.set("details", "true");
  else url.searchParams.delete("details");
  history.replaceState(history.state, "", url);
}, true);

document.addEventListener("htmx:configRequest", (event) => {
  const details = document.querySelector<HTMLDetailsElement>(
    "details[data-stats-details]",
  );
  if (!details?.open) return;
  // Not every control lives inside the details element, so explicitly carry
  // its open state into all stats requests.
  const detail = (event as CustomEvent).detail;
  detail.parameters.set("details", "true");
});
