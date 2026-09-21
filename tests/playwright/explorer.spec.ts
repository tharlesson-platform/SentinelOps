import { test, expect, Page } from "@playwright/test";
async function fixture(page: Page) {
  await page.addInitScript(() =>
    sessionStorage.setItem("sentinel-token", "fixture-only"),
  );
  const source = { state: "available", fetchedAt: new Date().toISOString() };
  const dashboard = {
    uid: "test-view",
    title: "Visão de processos",
    templating: {
      list: [
        {
          name: "environment",
          type: "query",
          includeAll: true,
          allValue: ".*",
          current: { value: "production" },
        },
        { name: "search", type: "textbox", current: { value: ".*" } },
      ],
    },
    panels: [
      {
        id: 1,
        title: "Uso fracionário",
        type: "stat",
        datasource: { type: "prometheus" },
        fieldConfig: { defaults: { unit: "percentunit" } },
        options: {},
        targets: [{ refId: "A", expr: "ratio" }],
      },
    ],
  };
  await page.route("**/api/v1/**", async (r) => {
    const u = new URL(r.request().url()),
      p = u.pathname;
    let data: unknown = [];
    const end = new Date(
        Number(u.searchParams.get("end") || Date.now() / 1000) * 1000,
      ).toISOString(),
      start = new Date(Date.parse(end) - 3600000).toISOString();
    const result = {
      type: "matrix",
      series: ["a", "b"].map((x, i) => ({
        labels: { instance: "host-" + x },
        points: [{ timestamp: end, value: 0.25 + i / 2 }],
      })),
      truncated: false,
      nonfinite: 0,
    };
    if (p.endsWith("/explorer/catalog"))
      data = {
        metrics: Array.from({ length: 85 }, (_, i) => ({
          name: `process_metric_${String(i).padStart(2, "0")}`,
          type: "counter",
          unit: "",
          help: "Métrica de teste",
          domain: "Processos",
        })),
        dashboards: [dashboard],
        panelCount: 1,
        sources: { prometheus_names: source, prometheus_metadata: source },
        truncated: false,
        catalogNote: "Definições de teste",
      };
    else if (p.endsWith("/dimensions"))
      data = {
        labels: { instance: ["host-a", "host-b"] },
        seriesCount: 2,
        truncated: false,
        source,
      };
    else if (p.endsWith("/metric"))
      data = {
        result,
        query: "process_metric_01",
        filters: JSON.parse(u.searchParams.get("filters") || "{}"),
        source,
        start,
        end,
        stepSeconds: 15,
      };
    else if (p.includes("/variables/"))
      data = { values: ["production", "test"], source, truncated: false };
    else if (p.includes("/panels/"))
      data = {
        panel: dashboard.panels[0],
        targets: [
          {
            refId: "A",
            sourceName: "prometheus",
            source,
            query: "ratio",
            appliedFilters: JSON.parse(u.searchParams.get("variables") || "{}"),
            result,
          },
        ],
        start,
        end,
        stepSeconds: 15,
      };
    else if (p.endsWith("/profiles/catalog"))
      data = {
        types: [
          {
            ID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
            name: "process_cpu",
            sampleType: "cpu",
            sampleUnit: "nanoseconds",
          },
        ],
        labels: ["service_name", "hostname"],
        sources: { types: source, labels: source },
      };
    else if (p.endsWith("/profiles/labels"))
      data = {
        values:
          u.searchParams.get("label") === "service_name"
            ? ["fixture-api"]
            : ["host-a", "host-b"],
        source,
        truncated: false,
      };
    else if (p.endsWith("/profiles/query"))
      data = {
        graph: {
          total: "100",
          limited: true,
          frames: [
            { name: "root", depth: 0, offset: "0", total: "100", self: "0" },
            {
              name: "processRequest",
              depth: 1,
              offset: "20",
              total: "40",
              self: "10",
            },
          ],
        },
        unit: "nanoseconds",
        selector: '{service_name="fixture-api"}',
        source,
        start,
        end,
        note: "Perfil de teste",
      };
    else if (p.endsWith("/overview"))
      data = {
        values: { hostsUp: 2, containers: 2, services: null },
        sources: { prometheus: source },
      };
    await r.fulfill({ json: { data } });
  });
}
test.beforeEach(async ({ page }) => fixture(page));
test("catálogo pesquisa e pagina, filtro só consulta quando solicitado", async ({
  page,
}) => {
  const calls: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/explorer/metric?")) calls.push(r.url());
  });
  await page.goto("/sentinelops/?page=explorer&explorerMode=metrics");
  await expect(
    page.getByText("85 nomes de métricas", { exact: false }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Próxima página" }).click();
  await expect(
    page.getByRole("button", { name: /process_metric_40/ }),
  ).toBeVisible();
  await page
    .getByLabel("Pesquisar métrica ou descrição")
    .fill("process_metric_01");
  await page.getByRole("button", { name: /process_metric_01/ }).click();
  await expect.poll(() => calls.length).toBe(1);
  await page.getByLabel("Instância da fonte").selectOption("host-b");
  await expect(
    page.getByText(
      "Filtros alterados. Selecione Consultar métrica para aplicá-los.",
    ),
  ).toBeVisible();
  expect(calls).toHaveLength(1);
  await page.getByRole("button", { name: "Consultar métrica" }).click();
  await expect.poll(() => calls.length).toBe(2);
  expect(JSON.parse(new URL(calls[1]).searchParams.get("filters")!)).toEqual({
    instance: "host-b",
  });
  await expect(page.locator(".series-legend")).toContainText("host-a");
  await expect(page.locator(".series-legend")).toContainText("host-b");
});
test("painel nativo preserva default, fração, literal e reload", async ({
  page,
}) => {
  const calls: URL[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/panels/")) calls.push(new URL(r.url()));
  });
  await page.goto("/sentinelops/?page=explorer");
  await page.getByRole("button", { name: /Visão de processos/ }).click();
  await expect(page.getByLabel("Ambiente", { exact: true })).toHaveValue(
    "production",
  );
  await page.getByRole("button", { name: /Uso fracionário/ }).click();
  await expect(page.locator(".stat-series")).toContainText("fração (0–1)");
  expect(JSON.parse(calls[0].searchParams.get("variables")!).environment).toBe(
    "production",
  );
  await page.getByLabel("Texto literal (vazio: sem filtro)").fill('error.*"');
  expect(calls).toHaveLength(1);
  await page.getByRole("button", { name: "Consultar painel" }).click();
  await expect.poll(() => calls.length).toBe(2);
  expect(JSON.parse(calls[1].searchParams.get("variables")!).search).toBe(
    'error.*"',
  );
  await page.reload();
  await expect(
    page.getByLabel("Texto literal (vazio: sem filtro)"),
  ).toHaveValue('error.*"');
  await page.getByRole("button", { name: "Início", exact: true }).click();
  await expect(
    page.getByRole("heading", {
      name: "Sua operação, com contexto.",
      exact: true,
    }),
  ).toBeVisible();
});
test("perfil nativo tem seleção explícita, árvore e tabela acessível", async ({
  page,
}) => {
  await page.goto("/sentinelops/?page=profiles");
  await expect(
    page.getByRole("button", { name: "Consultar perfil" }),
  ).toBeDisabled();
  await page.getByLabel("Serviço de perfis").selectOption("fixture-api");
  await page
    .getByLabel("Tipo de perfil")
    .selectOption("process_cpu:cpu:nanoseconds:cpu:nanoseconds");
  await page.getByRole("button", { name: "Consultar perfil" }).click();
  await page
    .getByRole("button", {
      name: "processRequest, nível 1, total 40, próprio 10",
    })
    .click();
  await expect(
    page.getByText("total com chamadas: 40", { exact: false }),
  ).toBeVisible();
  await page.getByText("Tabela acessível de funções e valores exatos").click();
  await expect(
    page.getByRole("cell", { name: "processRequest", exact: true }),
  ).toBeVisible();
});
test("explorador mobile aceita teclado e mantém a página dentro da largura", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/sentinelops/?page=explorer&explorerMode=metrics");
  await page.getByLabel("Pesquisar métrica ou descrição").focus();
  await page.keyboard.type("process_metric_01");
  await page.keyboard.press("Tab");
  await page.getByRole("button", { name: /process_metric_01/ }).click();
  await expect(
    page.getByRole("heading", { name: "process_metric_01" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "artifacts/explorer-mobile.png",
    fullPage: true,
  });
});
