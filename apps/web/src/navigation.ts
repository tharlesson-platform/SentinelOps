import { useEffect, useRef, useState } from "react";

export const pages = [
  "overview",
  "hosts",
  "docker",
  "metrics",
  "explorer",
  "logs",
  "apm",
  "traces",
  "profiles",
  "catalog",
  "tests",
  "releases",
  "agents",
  "governance",
  "help",
] as const;
export type Page = (typeof pages)[number];
export type EntityContext = {
  host?: string;
  hostName?: string;
  container?: string;
  containerId?: string;
  service?: string;
  serviceEnvironment?: string;
  apmResource?: string;
};
export type Route = {
  page: Page;
  context: EntityContext;
  window: string;
  end: number;
  query: string;
  hostFilter: string;
  environment: string;
  logSearch: string;
  severity: string;
  errors: boolean;
  traceStatus?: string;
  explorerMode?: string;
  metric?: string;
  dashboard?: string;
  panel?: string;
  filters?: string;
  variables?: string;
  operation?: string;
  profileType?: string;
  profileFilters?: string;
};
const entityKeys = [
  "host",
  "hostName",
  "container",
  "containerId",
  "service",
  "serviceEnvironment",
  "apmResource",
] as const;
export function readRoute(search = location.search): Route {
  const p = new URLSearchParams(search);
  const context: EntityContext = {};
  for (const key of entityKeys) if (p.get(key)) context[key] = p.get(key)!;
  return {
    page: pages.includes(p.get("page") as Page)
      ? (p.get("page") as Page)
      : "overview",
    context,
    window: ["15m", "1h", "6h", "24h"].includes(p.get("window") || "")
      ? p.get("window")!
      : "1h",
    end:
      Number(p.get("end")) > 0 && Number.isFinite(Number(p.get("end")))
        ? Number(p.get("end"))
        : Math.floor(Date.now() / 1000),
    query: p.get("q") || "",
    hostFilter: p.get("filterHost") || "",
    environment: p.get("environment") || "",
    logSearch: p.get("search") || "",
    severity: p.get("severity") || "",
    errors: p.get("errors") === "true",
    // Preserve unknown values so the API rejects them instead of widening scope.
    traceStatus: p.get("traceStatus") || (p.get("errors") === "true" ? "span-error" : "all"),
    ...Object.fromEntries(
      [
        "explorerMode",
        "metric",
        "dashboard",
        "panel",
        "filters",
        "variables",
        "operation",
        "profileType",
        "profileFilters",
      ].map((k) => [k, p.get(k) || ""]),
    ),
  };
}
export function routeSearch(r: Route) {
  const p = new URLSearchParams({
    page: r.page,
    window: r.window,
    end: String(r.end),
  });
  for (const key of entityKeys) if (r.context[key]) p.set(key, r.context[key]!);
  for (const [key, value] of Object.entries({
    q: r.query,
    filterHost: r.hostFilter,
    environment: r.environment,
    search: r.logSearch,
    severity: r.severity,
    errors: r.errors ? "true" : "",
    traceStatus: r.traceStatus,
    explorerMode: r.explorerMode,
    metric: r.metric,
    dashboard: r.dashboard,
    panel: r.panel,
    filters: r.filters,
    variables: r.variables,
    operation: r.operation,
    profileType: r.profileType,
    profileFilters: r.profileFilters,
  }))
    if (value) p.set(key, value);
  return `?${p}`;
}
export function useNavigation() {
  const [route, setRoute] = useState(readRoute);
  const current = useRef(route);
  const stopRestoring = useRef<() => void>(() => {});
  useEffect(() => {
    const prior = history.scrollRestoration;
    history.scrollRestoration = "manual";
    history.replaceState(
      { ...history.state, sentinel: true, depth: history.state?.depth || 0 },
      "",
    );
    const pop = () => {
      stopRestoring.current();
      const next = readRoute();
      current.current = next;
      setRoute(next);
      const target = history.state?.scrollY || 0;
      const observer = new ResizeObserver(() => {
        if (document.documentElement.scrollHeight - innerHeight >= target) {
          window.scrollTo(0, target);
          stopRestoring.current();
        }
      });
      const timer = window.setTimeout(() => stopRestoring.current(), 20000);
      stopRestoring.current = () => {
        observer.disconnect();
        window.clearTimeout(timer);
        cancelAnimationFrame(frame);
      };
      const frame = requestAnimationFrame(() => {
        window.scrollTo(0, target);
        observer.observe(document.body);
      });
    };
    const cancel = () => stopRestoring.current();
    window.addEventListener("wheel", cancel, { passive: true });
    window.addEventListener("touchstart", cancel, { passive: true });
    window.addEventListener("keydown", cancel);
    window.addEventListener("popstate", pop);
    return () => {
      stopRestoring.current();
      window.removeEventListener("popstate", pop);
      window.removeEventListener("wheel", cancel);
      window.removeEventListener("touchstart", cancel);
      window.removeEventListener("keydown", cancel);
      history.scrollRestoration = prior;
    };
  }, []);
  function update(patch: Partial<Route>, replace = false) {
    stopRestoring.current();
    const next = { ...current.current, ...patch };
    current.current = next;
    history.replaceState({ ...history.state, scrollY: window.scrollY }, "");
    const state = {
      sentinel: true,
      depth: (history.state?.depth || 0) + (replace ? 0 : 1),
      scrollY: replace ? window.scrollY : 0,
    };
    history[replace ? "replaceState" : "pushState"](
      state,
      "",
      `${location.pathname}${routeSearch(next)}`,
    );
    setRoute(next);
    if (!replace) window.scrollTo(0, 0);
  }
  function back() {
    if (history.state?.sentinel && history.state.depth > 0) history.back();
    else
      update({
        page: route.context.container
          ? "docker"
          : route.context.host
            ? "hosts"
            : "overview",
      });
  }
  return { route, update, back };
}
