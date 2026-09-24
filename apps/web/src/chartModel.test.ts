import { describe, it, expect } from "vitest";
import {
  formatMeasurement,
  resourceMetrics,
  seriesPoints,
  utilizationState,
} from "./chartModel";
const time = "2026-09-21T14:00:00Z";
const row = (
  value: number,
  labels: Record<string, string> = {},
  timestamp = time,
) => ({ labels, points: [{ timestamp, value }] });
describe("leitura operacional de capacidade", () => {
  it("mostra percentuais fracionários, bytes e taxas sem perder unidades", () => {
    expect(formatMeasurement(0.456, "fração (0–1)")).toBe("45,6 %");
    expect(formatMeasurement(1073741824, "bytes")).toBe("1 GiB");
    expect(formatMeasurement(2048, "bytes/s")).toBe("2 KiB/s");
    expect(formatMeasurement(0.025, "segundos")).toBe("25 ms");
    expect(formatMeasurement(0, "%")).toBe("0 %");
    expect(formatMeasurement(NaN, "%")).toBe("Sem dados");
  });
  it("calcula CPU ocupada usando idle, não a soma de modos que pode duplicar guest", () => {
    const m = resourceMetrics(
      {
        cpuByMode: [
          row(65, { mode: "idle" }),
          row(30, { mode: "user" }),
          row(12, { mode: "guest" }),
        ],
      },
      false,
    );
    expect(m.cpuUsed[0].points[0].value).toBe(35);
  });
  it("percentual de memória requer limite do mesmo recurso e timestamp", () => {
    const labels = { instance: "tqi-platform", name: "api", id: "a" };
    const m = resourceMetrics(
      {
        memory: [row(75, labels)],
        memoryLimit: [row(100, labels), row(1000, { ...labels, id: "b" })],
      },
      true,
    );
    expect(m.memoryPercent[0].points[0].value).toBe(75);
    expect(
      resourceMetrics(
        { memory: [row(75, labels)], memoryLimit: [row(0, labels)] },
        true,
      ).memoryPercent[0].points[0].value,
    ).toBeNaN();
    expect(
      resourceMetrics(
        {
          memory: [row(75, labels)],
          memoryLimit: [row(100, labels, "2026-09-21T13:59:45Z")],
        },
        true,
      ).memoryPercent[0].points[0].value,
    ).toBeNaN();
    expect(
      resourceMetrics(
        {
          memory: [row(75, labels)],
          memoryLimit: [row(100, { ...labels, id: "b" })],
        },
        true,
      ).memoryPercent,
    ).toEqual([]);
  });
  it("não promove ausência, amostra antiga ou fora da janela a estado normal", () => {
    expect(utilizationState(undefined, false).tone).toBe("unknown");
    expect(utilizationState(10, true).tone).toBe("unknown");
    expect(utilizationState(0, false).tone).toBe("normal");
    expect(utilizationState(90, false).tone).toBe("critical");
    expect(
      seriesPoints(row(10), Date.parse(time) + 1, Date.parse(time) + 1000),
    ).toEqual([]);
  });
});
it("swap sem capacidade não vira100%; capacidade e livre usam a mesma grade", () => {
  const labels = { instance: "host" };
  expect(
    resourceMetrics(
      { swapUsed: [row(100, labels)], swapCapacity: [row(0, labels)] },
      false,
    ).swapUsed[0].points[0].value,
  ).toBeNaN();
  const later = "2026-09-21T14:01:01Z";
  const m = resourceMetrics(
    {
      swapCapacity: [
        {
          labels,
          points: [
            ...row(100, labels).points,
            ...row(100, labels, later).points,
          ],
        },
      ],
      swapFree: [
        {
          labels,
          points: [...row(80, labels).points, ...row(65, labels, later).points],
        },
      ],
    },
    false,
  );
  expect(m.swapUsed[0].points[0].value).toBeCloseTo(20);
  expect(m.swapUsed[0].points[1].value).toBe(35);
  expect(m.swapUsed[0].points[1].timestamp).toBe(later);
});
