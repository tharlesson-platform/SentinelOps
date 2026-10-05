import { useState } from "react";
import type { TelemetrySeries } from "./api";
import {
  formatMeasurement,
  seriesPoints,
  seriesName,
  utilizationState,
} from "./chartModel";
const colors = [
  "#1682c4",
  "#c44675",
  "#8366cc",
  "#b58116",
  "#079785",
  "#657e91",
  "#d96037",
  "#4f7c25",
];
export function chartSegments(
  series: TelemetrySeries,
  start: number,
  end: number,
  max: number,
  step: number,
  min = 0,
) {
  const segments: string[][] = [];
  let segment: string[] = [];
  let previous: number | undefined;
  for (const p of [...series.points].sort(
    (a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp),
  )) {
    const t = Date.parse(p.timestamp);
    if (
      p.value == null ||
      !Number.isFinite(p.value) ||
      !Number.isFinite(t) ||
      t < start ||
      t > end
    ) {
      if (segment.length) segments.push(segment);
      segment = [];
      previous = undefined;
      continue;
    }
    if (previous !== undefined && t - previous > step * 1500) {
      if (segment.length) segments.push(segment);
      segment = [];
    }
    segment.push(
      `${(((t - start) / Math.max(end - start, 1)) * 480).toFixed(2)},${(100 - ((p.value - min) / (max > min ? max - min : 1)) * 90).toFixed(2)}`,
    );
    previous = t;
  }
  if (segment.length) segments.push(segment);
  return segments;
}
export function TelemetryChart({
  name,
  series,
  start,
  end,
  step,
  meta,
  legendFormat,
}: {
  name: string;
  series: TelemetrySeries[];
  start: string;
  end: string;
  step: number;
  meta?: [string, string, string];
  legendFormat?: string;
}) {
  const [hidden, setHidden] = useState<number[]>([]);
  const [cursor, setCursor] = useState<number | null>(null);
  const [label, unit, description] = meta || [
    name,
    "valor",
    "Série informada pela fonte.",
  ];
  const begin = Date.parse(start),
    finish = Date.parse(end);
  const rows = series.map((s, i) => {
    const points = seriesPoints(s, begin, finish);
    return {
      s,
      i,
      points,
      last: points.at(-1),
      label: seriesName(s.labels, legendFormat),
    };
  });
  const visible = rows.filter((r) => !hidden.includes(r.i));
  const values = visible.flatMap((r) => r.points.map((p) => p.value!));
  const fractional = unit === "fração (0–1)" || unit === "percentunit";
  const percentage =
    unit === "%" || unit === "% (0–100)" || unit === "percent" || fractional;
  const min = Math.min(0, ...values),
    max = Math.max(percentage ? (fractional ? 1 : 100) : 1, ...values);
  const capacity =
    !meta?.[0]?.includes("por modo") &&
    ["cpuUsed", "memoryUsed", "memoryPercent", "diskUsed"].includes(name);
  const latest = rows.flatMap((r) => (r.last ? [r.last] : []));
  const value = latest.length
    ? Math.max(...latest.map((p) => p.value!))
    : undefined;
  const incomplete = rows.some((r) => !r.last);
  const stale = latest.some(
    (p) => finish - Date.parse(p.timestamp) > step * 2000,
  );
  const state = incomplete
    ? { tone: "unknown", label: "Cobertura incompleta" }
    : utilizationState(value, stale);
  const inspected = cursor == null ? null : begin + cursor * (finish - begin);
  const readAt = (row: (typeof rows)[number]) => {
    if (inspected == null) return undefined;
    const nearest = row.points.reduce<(typeof row.points)[number] | undefined>(
      (a, p) =>
        !a ||
        Math.abs(Date.parse(p.timestamp) - inspected) <
          Math.abs(Date.parse(a.timestamp) - inspected)
          ? p
          : a,
      undefined,
    );
    return nearest &&
      Math.abs(Date.parse(nearest.timestamp) - inspected) <= step * 500
      ? nearest
      : undefined;
  };
  return (
    <article className={`mini-chart noc-chart ${capacity ? state.tone : ""}`}>
      <header>
        <b>{label}</b>
        <span>Série temporal</span>
      </header>
      <div className="chart-reading">
        <strong>{formatMeasurement(value, unit)}</strong>
        <span>
          {latest.length > 1
            ? "Maior valor entre todas as séries"
            : "Última amostra no período"}
          {stale ? " · antiga" : ""}
        </span>
        {capacity && (
          <b className={`usage-state ${state.tone}`}>
            {state.label} · no período
          </b>
        )}
      </div>
      {capacity && value != null && (
        <div
          className="capacity-bar"
          role="meter"
          aria-label={label}
          aria-valuemin={0}
          aria-valuemax={Math.max(100, value)}
          aria-valuenow={value}
          aria-valuetext={formatMeasurement(value, unit)}
        >
          <span style={{ width: `${Math.min(100, Math.max(0, value))}%` }} />
        </div>
      )}
      <p>{description}</p>
      {capacity && (
        <small className="threshold-guide">
          Referência visual: atenção ≥80% · uso muito alto ≥90%. Não substitui
          as regras de alerta.
        </small>
      )}
      {values.length ? (
        <svg
          viewBox="0 0 600 155"
          role="slider"
          tabIndex={0}
          aria-label={`${label}: inspecionar horário com as setas`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round((cursor ?? 1) * 100)}
          aria-valuetext={new Date(inspected ?? finish).toLocaleString("pt-BR")}
          onPointerMove={(e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            setCursor(
              Math.max(
                0,
                Math.min(
                  1,
                  (((e.clientX - rect.left) / rect.width) * 600 - 100) / 480,
                ),
              ),
            );
          }}
          onPointerLeave={() => setCursor(null)}
          onKeyDown={(e) => {
            if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
              e.preventDefault();
              setCursor(
                Math.max(
                  0,
                  Math.min(
                    1,
                    (cursor ?? 1) +
                      ((e.key === "ArrowLeft" ? -1 : 1) * step * 1000) /
                        Math.max(1, finish - begin),
                  ),
                ),
              );
            }
          }}
        >
          {[0, 0.25, 0.5, 0.75, 1].map((f) => (
            <g key={f}>
              <line
                className="chart-gridline"
                x1="100"
                x2="580"
                y1={105 - f * 90}
                y2={105 - f * 90}
              />
              <text
                className="chart-axis"
                x="92"
                y={109 - f * 90}
                textAnchor="end"
              >
                {formatMeasurement(min + (max - min) * f, unit)}
              </text>
            </g>
          ))}
          {capacity &&
            [80, 90].map((v) => (
              <line
                key={v}
                className={`threshold-line threshold-${v}`}
                x1="100"
                x2="580"
                y1={105 - ((v - min) / (max - min)) * 90}
                y2={105 - ((v - min) / (max - min)) * 90}
              />
            ))}
          <g transform="translate(100 5)">
            {visible.map(({ s, i }) =>
              chartSegments(s, begin, finish, max, step, min).map(
                (segment, j) => (
                  <g key={`${i}-${j}`}>
                    <polyline
                      style={{ stroke: colors[i % colors.length] }}
                      points={segment.join(" ")}
                    />
                    {segment.length === 1 && (
                      <circle
                        cx={segment[0].split(",")[0]}
                        cy={segment[0].split(",")[1]}
                        r="3"
                        fill={colors[i % colors.length]}
                      />
                    )}
                  </g>
                ),
              ),
            )}
          </g>
          {[0, 0.5, 1].map((f) => (
            <text
              key={f}
              className="chart-axis"
              x={100 + 480 * f}
              y="130"
              textAnchor={f === 0 ? "start" : f === 1 ? "end" : "middle"}
            >
              {new Date(begin + (finish - begin) * f).toLocaleTimeString(
                "pt-BR",
              )}
            </text>
          ))}
          {cursor != null && (
            <line
              className="chart-cursor"
              x1={100 + cursor * 480}
              x2={100 + cursor * 480}
              y1="12"
              y2="108"
            />
          )}
        </svg>
      ) : (
        <div className="chart-no-data">
          {hidden.length && rows.length
            ? "Todas as séries ocultas. Ative uma série abaixo."
            : "Sem amostras no período — não significa uso zero."}
        </div>
      )}
      {inspected != null && (
        <div className="chart-tooltip" aria-live="polite">
          <b>{new Date(inspected).toLocaleString("pt-BR")}</b>
          {visible.map((r) => (
            <span key={r.i}>
              <i style={{ background: colors[r.i % colors.length] }} />
              {r.label}:{" "}
              <strong>{formatMeasurement(readAt(r)?.value, unit)}</strong>
              {readAt(r) && (
                <small>
                  amostra às{" "}
                  {new Date(readAt(r)!.timestamp).toLocaleTimeString("pt-BR")}
                </small>
              )}
            </span>
          ))}
        </div>
      )}
      <div className="chart-legend-wrap">
        <table className="chart-legend">
          <thead>
            <tr>
              <th>Série · clique para ocultar</th>
              <th>Último</th>
              <th>Mín.</th>
              <th>Média</th>
              <th>Máx.</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const vals = r.points.map((p) => p.value!);
              return (
                <tr
                  key={r.i}
                  className={hidden.includes(r.i) ? "muted-series" : ""}
                >
                  <td>
                    <button
                      aria-pressed={!hidden.includes(r.i)}
                      title={Object.entries(r.s.labels)
                        .map(([k, v]) => `${k}=${v}`)
                        .join(" · ")}
                      onClick={() =>
                        setHidden((h) =>
                          h.includes(r.i)
                            ? h.filter((i) => i !== r.i)
                            : [...h, r.i],
                        )
                      }
                    >
                      <i style={{ background: colors[r.i % colors.length] }} />
                      {r.label}
                    </button>
                    <small>
                      {r.last
                        ? new Date(r.last.timestamp).toLocaleTimeString("pt-BR")
                        : "Sem amostras"}
                      {r.last &&
                      finish - Date.parse(r.last.timestamp) > step * 2000
                        ? " · antiga"
                        : ""}
                    </small>
                  </td>
                  <td>{formatMeasurement(r.last?.value, unit)}</td>
                  <td>
                    {formatMeasurement(
                      vals.length ? Math.min(...vals) : null,
                      unit,
                    )}
                  </td>
                  <td>
                    {formatMeasurement(
                      vals.length
                        ? vals.reduce((a, b) => a + b, 0) / vals.length
                        : null,
                      unit,
                    )}
                  </td>
                  <td>
                    {formatMeasurement(
                      vals.length ? Math.max(...vals) : null,
                      unit,
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <details>
        <summary>Ver dados em tabela e labels da fonte</summary>
        <div className="chart-data">
          <table>
            <thead>
              <tr>
                <th>Série / labels</th>
                <th>Horário</th>
                <th>Valor sem formatação ({unit})</th>
              </tr>
            </thead>
            <tbody>
              {series.flatMap((s, i) =>
                s.points.map((p, j) => (
                  <tr key={`${i}-${j}`}>
                    <td>
                      {Object.entries(s.labels)
                        .map(([k, v]) => `${k}=${v}`)
                        .join(" · ") || "Total"}
                    </td>
                    <td>{new Date(p.timestamp).toLocaleString("pt-BR")}</td>
                    <td>
                      {p.value == null || !Number.isFinite(p.value)
                        ? "Sem dados"
                        : p.value.toLocaleString("pt-BR", {
                            maximumSignificantDigits: 12,
                          })}
                    </td>
                  </tr>
                )),
              )}
            </tbody>
          </table>
        </div>
      </details>
    </article>
  );
}
