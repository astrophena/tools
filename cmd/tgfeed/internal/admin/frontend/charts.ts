import {
  BarController,
  BarElement,
  CategoryScale,
  Chart,
  ChartConfiguration,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  Tooltip,
} from "chart.js";
import htmx, { HtmxExtension } from "htmx.org";

Chart.register(
  BarController,
  BarElement,
  CategoryScale,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  Tooltip,
);

const hx = htmx as unknown as {
  ajax: (
    verb: "GET",
    path: string,
    context: Record<string, unknown>,
  ) => Promise<void>;
  defineExtension: (name: string, extension: Partial<HtmxExtension>) => void;
};

type ChartSpec = {
  config: ChartConfiguration;
  preset?: string;
  times?: string[];
  select_urls?: string[];
};

const fallback = "This chart can't be rendered.";
const charts = new WeakMap<HTMLElement, Chart>();
const observers = new WeakMap<HTMLElement, ResizeObserver>();
let registered = false;

function fail(container: HTMLElement, error?: unknown): void {
  if (error) console.error("Chart rendering failed", error);
  const message = document.createElement("p");
  message.className = "chart-empty-hint";
  message.textContent = fallback;
  container.replaceChildren(message);
}

function render(container: HTMLElement): void {
  try {
    const raw = container.dataset.chartSpec;
    if (!raw) throw new Error("chart specification is missing");
    const spec = JSON.parse(raw) as ChartSpec;
    if (!spec.config || typeof spec.config.type !== "string") {
      throw new Error("chart specification is invalid");
    }
    if (spec.times?.length) {
      spec.config.data.labels = spec.times.map((value) => {
        const date = new Date(value);
        return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(
          undefined,
          {
            day: "2-digit",
            month: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
            hour12: false,
          },
        ).format(date);
      });
    }
    const options = spec.config.options ??= {};
    options.responsive ??= true;
    options.maintainAspectRatio ??= false;
    options.plugins ??= {};
    options.plugins.legend ??= {
      labels: { color: "#dbe8ef", usePointStyle: true, boxWidth: 14 },
    };
    options.scales ??= {};
    options.scales.x ??= {
      ticks: { color: "#9db1be", maxRotation: 0 },
      grid: { color: "rgba(157, 177, 190, 0.08)" },
    };
    options.scales.y ??= {
      beginAtZero: true,
      ticks: { color: "#9db1be" },
      grid: { color: "rgba(157, 177, 190, 0.14)" },
    };
    if (spec.preset === "seconds") {
      options.scales ??= {};
      const y = options.scales.y ??= {};
      y.ticks ??= {};
      y.ticks.callback = (value) => formatSeconds(Number(value));
    }
    if (spec.select_urls?.length) {
      options.onClick = (_event, elements) => {
        const index = elements[0]?.index;
        const path = index === undefined
          ? undefined
          : spec.select_urls?.[index];
        if (path) {
          void hx.ajax("GET", path, {
            target: "#stats-content",
            swap: "outerHTML",
            push: path,
          });
        }
      };
    }
    const canvas = document.createElement("canvas");
    container.replaceChildren(canvas);
    charts.set(container, new Chart(canvas, spec.config));
  } catch (error) {
    fail(container, error);
  }
}

function initialize(container: HTMLElement): void {
  if (charts.has(container) || observers.has(container)) return;
  if (container.getClientRects().length > 0 && container.clientWidth > 0) {
    render(container);
    return;
  }
  // Charts inside a closed details element have no usable size. Defer their
  // construction until the container becomes visible instead of rendering a
  // permanently zero-sized canvas.
  if (!("ResizeObserver" in globalThis)) {
    fail(container, new Error("ResizeObserver is unavailable"));
    return;
  }
  const observer = new ResizeObserver(() => {
    if (
      container.getClientRects().length === 0 || container.clientWidth === 0
    ) return;
    observer.disconnect();
    observers.delete(container);
    render(container);
  });
  observers.set(container, observer);
  observer.observe(container);
}

function process(root: HTMLElement): void {
  if (root.matches("[data-chart-spec]")) initialize(root);
  root.querySelectorAll<HTMLElement>("[data-chart-spec]").forEach(initialize);
}

function cleanup(root: HTMLElement): void {
  const containers = root.matches("[data-chart-spec]")
    ? [root]
    : Array.from(root.querySelectorAll<HTMLElement>("[data-chart-spec]"));
  for (const container of containers) {
    charts.get(container)?.destroy();
    observers.get(container)?.disconnect();
    charts.delete(container);
    observers.delete(container);
  }
}

export function registerCharts(root: ParentNode = document): void {
  if (!registered) {
    // Register once for future HTMX swaps; the explicit processing below also
    // covers the initial document and a fragment that triggered lazy import.
    registered = true;
    hx.defineExtension("chart", {
      onEvent(name, event) {
        const element = (event as CustomEvent).detail?.elt as
          | HTMLElement
          | undefined;
        if (name === "htmx:afterProcessNode" && element) process(element);
        if (name === "htmx:beforeCleanupElement" && element) cleanup(element);
        return true;
      },
    });
  }
  if (root instanceof HTMLElement) process(root);
  else {root.querySelectorAll<HTMLElement>("[data-chart-spec]").forEach(
      initialize,
    );}
}

function formatSeconds(seconds: number): string {
  if (seconds < 1) return `${Math.round(seconds * 1000)} ms`;
  if (seconds < 60) return `${seconds.toFixed(1).replace(/\.0$/, "")} s`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ${Math.round(seconds % 60)}s`;
}
