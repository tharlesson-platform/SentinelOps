import type { TelemetrySeries } from "./api";

export function formatMeasurement(
  value: number | null | undefined,
  unit: string,
): string {
  if (value == null || !Number.isFinite(value)) return "Sem dados";
  let n = value,
    suffix = unit;
  if (unit === "fração (0–1)" || unit === "percentunit") {
    n *= 100;
    suffix = "%";
  }
  if (unit === "% (0–100)" || unit === "percent") suffix = "%";
  if (unit === "bytes" || unit === "bytes/s" || unit === "Bps") {
    const exponent = Math.min(
      4,
      Math.max(0, Math.floor(Math.log2(Math.abs(n) || 1) / 10)),
    );
    n /= 1024 ** exponent;
    suffix =
      ["B", "KiB", "MiB", "GiB", "TiB"][exponent] +
      (unit !== "bytes" ? "/s" : "");
  }
  if ((unit === "s" || unit === "segundos") && Math.abs(n) < 1 && n !== 0) {
    n *= 1000;
    suffix = "ms";
  }
  if (
    suffix === "valor" ||
    suffix === "short" ||
    suffix === "unidade não informada"
  )
    suffix = "";
  return `${n.toLocaleString("pt-BR", { maximumFractionDigits: 2, ...(n !== 0 && Math.abs(n) < 0.01 ? { maximumSignificantDigits: 3 } : {}) })}${suffix ? " " + suffix : ""}`;
}
export function seriesPoints(
  series: TelemetrySeries,
  start: number,
  end: number,
) {
  return series.points
    .filter(
      (p) =>
        p.value != null &&
        Number.isFinite(p.value) &&
        Date.parse(p.timestamp) >= start &&
        Date.parse(p.timestamp) <= end,
    )
    .sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp));
}
export function seriesName(labels: Record<string, string>, legend?: string) {
  const rendered = legend?.replace(
    /\{\{\s*([^}]+?)\s*\}\}/g,
    (_, key) => labels[key] || "",
  );
  if (rendered?.trim()) return rendered;
  const modes: Record<string, string> = {
    idle: "Ocioso",
    user: "Aplicações",
    system: "Sistema",
    iowait: "Espera por disco",
    steal: "Espera pelo hipervisor",
    irq: "Interrupções",
    softirq: "Interrupções de software",
    nice: "Aplicações de baixa prioridade",
  };
  if (labels.mode) return modes[labels.mode] || labels.mode;
  const main = [
    labels.mountpoint || labels.device,
    labels.service_name || labels.name,
    labels.instance || labels.host_name,
  ]
    .filter(Boolean)
    .join(" · ");
  const extra = Object.entries(labels)
    .filter(([k]) =>
      ["job", "http_route", "route", "status_code", "code", "le"].includes(k),
    )
    .map(([k, v]) => `${k}=${v}`)
    .join(" · ");
  return (
    [main, extra].filter(Boolean).join(" · ") ||
    Object.entries(labels)
      .filter(([k]) => k !== "__name__")
      .map(([k, v]) => `${k}=${v}`)
      .join(" · ") ||
    "Total"
  );
}
export function resourceMetrics(
  metrics: Record<string, TelemetrySeries[]>,
  container: boolean,
) {
  const result = { ...metrics };
  if (!container) {
    result.swapUsed = (metrics.swapCapacity || []).map((capacity) => {
      const free = (metrics.swapFree || []).filter(
        (s) => s.labels.instance === capacity.labels.instance,
      );
      const samples =
        free.length === 1
          ? new Map(free[0].points.map((p) => [p.timestamp, p.value]))
          : new Map<string, number>();
      return {
        ...capacity,
        points: capacity.points.map((p) => {
          const available = samples.get(p.timestamp);
          return {
            ...p,
            value:
              p.value > 0 && available != null
                ? 100 * (1 - available / p.value)
                : NaN,
          };
        }),
      };
    });
    result.cpuUsed = (metrics.cpuByMode || [])
      .filter((s) => s.labels.mode === "idle")
      .map((s) => ({
        labels: {},
        points: s.points.map((p) => ({
          ...p,
          value: p.value == null ? NaN : 100 - p.value,
        })),
      }));
  } else {
    // Match the resource identity and exact timestamps; never divide unrelated or stale samples.
    const identity = (s: TelemetrySeries) =>
      JSON.stringify(["instance", "name", "id"].map((k) => s.labels[k] || ""));
    if (metrics.memory)
      result.memoryPercent = (metrics.memory || []).flatMap((s) => {
        const matches = (metrics.memoryLimit || []).filter(
          (l) => identity(l) === identity(s),
        );
        if (matches.length !== 1) return [];
        const limits = new Map(
          matches[0].points.map((p) => [p.timestamp, p.value]),
        );
        return [
          {
            labels: s.labels,
            points: s.points.map((p) => {
              const limit = limits.get(p.timestamp);
              return {
                ...p,
                value:
                  p.value != null && limit != null && limit > 0
                    ? (p.value / limit) * 100
                    : NaN,
              };
            }),
          },
        ];
      });
  }
  const order = container
    ? [
        "cpu",
        "memoryPercent",
        "memory",
        "memoryLimit",
        "networkRx",
        "networkTx",
        "filesystem",
        "throttling",
      ]
    : [
        "cpuUsed",
        "memoryUsed",
        "diskUsed",
        "networkRx",
        "networkTx",
        "diskRead",
        "diskWrite",
        "cpuByMode",
        "load1",
        "load5",
        "load15",
        "swapUsed",
      ];
  return Object.fromEntries(
    [
      ...order.filter((k) => k in result),
      ...Object.keys(result).filter(
        (k) => !order.includes(k) && !["swapCapacity", "swapFree"].includes(k),
      ),
    ].map((k) => [k, result[k]]),
  );
}
export function utilizationState(value: number | undefined, stale: boolean) {
  if (value == null || stale)
    return { tone: "unknown", label: stale ? "Amostra antiga" : "Sem dados" };
  if (value >= 90) return { tone: "critical", label: "Uso muito alto" };
  if (value >= 80) return { tone: "warning", label: "Uso elevado" };
  return { tone: "normal", label: "Abaixo de 80%" };
}
