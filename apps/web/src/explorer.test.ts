import { describe, it, expect } from "vitest";
import {
  displayUnit,
  lastFinite,
  readSelection,
  profilePosition,
} from "./Explorer";
import { chartSegments } from "./Observability";
import { readRoute, routeSearch } from "./navigation";
describe("fidelidade das fontes", () => {
  it("não confunde percentagem e fração", () => {
    expect(displayUnit("percent")).toBe("% (0–100)");
    expect(displayUnit("percentunit")).toBe("fração (0–1)");
  });
  it("lastNotNull preserva zero e timestamp, ignora indefinidos", () => {
    expect(
      lastFinite({
        labels: {},
        points: [
          { timestamp: "2026-09-21T01:00:00Z", value: 10 },
          { timestamp: "2026-09-21T01:00:15Z", value: 0 },
          { timestamp: "2026-09-21T01:00:30Z", value: NaN },
        ],
      }),
    ).toEqual({ timestamp: "2026-09-21T01:00:15Z", value: 0 });
  });
  it("desenha valores negativos e positivos sem recortar", () => {
    const got = chartSegments(
      {
        labels: {},
        points: [
          { timestamp: new Date(0).toISOString(), value: -2 },
          { timestamp: new Date(15000).toISOString(), value: 2 },
        ],
      },
      0,
      15000,
      2,
      15,
      -2,
    );
    expect(got).toEqual([["0.00,100.00", "480.00,10.00"]]);
  });
  it("preserva escolhas e valores literais no histórico compartilhável", () => {
    const route = readRoute(
      "?page=explorer&metric=up&filters=" +
        encodeURIComponent(JSON.stringify({ job: 'x"|.*' })) +
        "&dashboard=demo&panel=2&profileType=process_cpu:cpu:nanoseconds:cpu:nanoseconds&profileFilters=" +
        encodeURIComponent('{"service_name":"api"}'),
    );
    expect(readRoute(routeSearch(route))).toEqual(route);
    expect(readSelection(route.filters).job).toBe('x"|.*');
  });
  it("posiciona filhos sem alterar valores originais", () => {
    const frame = {
      name: "child",
      depth: 1,
      offset: "20",
      total: "30",
      self: "10",
    };
    expect(profilePosition(frame, "100")).toEqual({ left: 20, width: 30 });
    expect(frame.self).toBe("10");
  });
});
