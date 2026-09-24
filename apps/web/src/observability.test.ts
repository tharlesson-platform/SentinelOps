import { describe, expect, it } from "vitest";
import { chartSegments, exploreURL } from "./Observability";
import { readRoute, routeSearch } from "./navigation";

describe("jornada compartilhável", () => {
  it("preserva entidade composta, filtros, busca e período ao recarregar", () => {
    const route = readRoute(
      "?page=docker&host=cadvisor%3A8080&hostName=node-1&container=api&containerId=abc&filterHost=node-1&q=api&window=6h&end=1800000000&environment=production&search=falha&severity=ERROR",
    );
    expect(readRoute(routeSearch(route))).toEqual(route);
    expect(route.context.host).toBe("cadvisor:8080");
  });
  it("valida página e janela recebidas na URL", () => {
    expect(readRoute("?page=bad&window=bad").page).toBe("overview");
    expect(readRoute("?window=bad").window).toBe("1h");
  });
  it("preserva classe HTTP, recurso e período em URL e links antigos", () => {
    for (const traceStatus of ["all", "span-error", "4xx", "5xx"]) {
      const route = readRoute(`?page=traces&service=api&hostName=node-a&container=web&window=6h&end=1800000000&traceStatus=${traceStatus}`);
      expect(readRoute(routeSearch(route))).toEqual(route);
      expect(route.traceStatus).toBe(traceStatus);
    }
    expect(readRoute("?page=traces&errors=true").traceStatus).toBe("span-error");
    expect(readRoute("?errors=true&traceStatus=4xx").traceStatus).toBe("4xx");
    expect(readRoute("?traceStatus=invalid").traceStatus).toBe("invalid");
  });
  it("envia ID do trace e limites absolutos à fonte configurada", () => {
    const url = new URL(
      exploreURL("tempo", { window: "1h", end: 1800000000 }, "abc"),
      "https://example.test",
    );
    const pane = JSON.parse(url.searchParams.get("panes")!).investigation;
    expect(pane.datasource).toBe("tempo");
    expect(pane.queries[0]).toMatchObject({
      query: "abc",
      queryType: "traceql",
    });
    expect(Number(pane.range.to) - Number(pane.range.from)).toBe(3600000);
  });
});
describe("gráficos fiéis ao tempo", () => {
  it("usa timestamp, preserva lacunas e ordena amostras", () => {
    const points = [
      { timestamp: new Date(0).toISOString(), value: 10 },
      { timestamp: new Date(60000).toISOString(), value: 20 },
      { timestamp: new Date(15000).toISOString(), value: 15 },
    ];
    const segments = chartSegments(
      { labels: { mode: "user" }, points },
      0,
      60000,
      20,
      15,
    );
    expect(segments).toHaveLength(2);
    expect(segments[0][1].split(",")[0]).toBe("120.00");
    expect(segments[1][0].split(",")[0]).toBe("480.00");
  });
  it("não transforma amostra inválida em linha ou zero", () => {
    expect(
      chartSegments(
        {
          labels: {},
          points: [{ timestamp: new Date(0).toISOString(), value: NaN }],
        },
        0,
        60000,
        100,
        15,
      ),
    ).toEqual([]);
  });
});

it("encaminha ambas as fontes de logs e preserva literal com aspas", () => {
  const expressions = [
    '{service_name=~".+"} |= "erro"',
    '{host_name=~".+",service_name=""} |= "erro"',
  ];
  const link = new URL(
    exploreURL("loki", { window: "1h", end: 1800000000 }, expressions),
    "https://example.test",
  );
  const queries = JSON.parse(link.searchParams.get("panes")!).investigation
    .queries;
  expect(queries.map((q: { expr: string }) => q.expr)).toEqual(expressions);
  expect(queries.map((q: { refId: string }) => q.refId)).toEqual(["A", "B"]);
});
