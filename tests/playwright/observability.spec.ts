import { test, expect, Page } from "@playwright/test";
import { readFileSync } from "node:fs";

// Controlled fixtures only: no production API, identity provider or telemetry.
const id = "a".repeat(64);
const containers = ["a", "b"].map((host) => ({
  name: "api",
  host: `cadvisor-${host}:8080`,
  hostName: `node-${host}`,
  id: `/docker/${id}`,
  logContainerId: id,
  image: "test-only",
  environment: "test",
  lastSeen: new Date().toISOString(),
  cpuPercent: 120,
  memoryBytes: 1024,
  memoryPercent: 1,
  restarts24h: 0,
  state: "running",
}));
const hosts = ["a", "b"].map((host) => ({
  name: `node-${host}`,
  instance: `exporter-${host}:9100`,
  hostName: `node-${host}`,
  environment: "test",
  cpuPercent: 10,
  memoryPercent: 20,
  diskPercent: 30,
  up: true,
  state: "up",
}));
function source(state = "available", old = false) {
  return {
    state,
    fetchedAt: new Date(Date.now() - (old ? 600000 : 0)).toISOString(),
  };
}
function detail(url: URL, value = 25) {
  const end = Number(url.searchParams.get("end"));
  return {
    host:
      url.searchParams.get("host") ||
      decodeURIComponent(url.pathname.split("/").at(-1)!),
    container: "api",
    start: new Date((end - 3600) * 1000).toISOString(),
    end: new Date(end * 1000).toISOString(),
    stepSeconds: 15,
    metrics: {
      [url.pathname.includes("/containers/") ? "cpu" : "cpuByMode"]: [
        {
          labels: { mode: "user" },
          points: [
            { timestamp: new Date((end - 15) * 1000).toISOString(), value },
            { timestamp: new Date(end * 1000).toISOString(), value },
          ],
        },
      ],
    },
    sources: { prometheus: source() },
  };
}
async function fixture(page: Page) {
  await page.addInitScript(() =>
    sessionStorage.setItem("sentinel-token", "fixture-only"),
  );
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const p = url.pathname;
    let data: unknown = [];
    if (p.endsWith("/observability/overview"))
      data = {
        values: { hostsUp: 2, containers: 2, services: null },
        sources: { prometheus: source() },
      };
    else if (p.endsWith("/observability/hosts"))
      data = { items: hosts, sources: { prometheus: source() } };
    else if (p.endsWith("/observability/containers"))
      data = { items: containers, sources: { prometheus: source() } };
    else if (/observability\/(hosts|containers)\//.test(p)) data = detail(url);
    else if (p.endsWith("/logs"))
      data = {
        items: [],
        query: `{host_name="${url.searchParams.get("host") || "all"}"}`,
        sources: { loki: source("no_data") },
      };
    else if (p.endsWith("/traces"))
      data = {
        items: [
          {
            traceId: "b".repeat(32),
            rootServiceName: "fixture-api",
            rootTraceName: "GET /fixture",
            durationMs: 20,
          },
        ],
        sources: { tempo: source() },
      };
    else if (p.endsWith("/apm"))
      data = { items: [], sources: { prometheus: source("no_data") } };
    else if (p.endsWith("/explorer/profiles/catalog"))
      data = {
        types: [],
        labels: [],
        sources: { types: source("no_data"), labels: source("no_data") },
      };
    else if (p.endsWith("/explorer/profiles/labels"))
      data = { values: [], source: source("no_data"), truncated: false };
    else if (p.endsWith("/assets/search")) data = { items: [] };
    await route.fulfill({ json: { data } });
  });
}
async function openContainers(page: Page) {
  await page.goto("/sentinelops/?page=docker");
  await expect(
    page.getByRole("heading", { name: "Containers", exact: true }),
  ).toBeVisible();
}
const resource = (page: Page, host: string) =>
  page.getByRole("button", { name: new RegExp(`api cadvisor-${host}`) });
test.beforeEach(async ({ page }) => {
  await fixture(page);
});
test("escolha explícita, homônimos, filtro global e container → métricas → logs → voltar → reload", async ({
  page,
}) => {
  const requests: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/observability/")) requests.push(r.url());
  });
  await openContainers(page);
  await expect(resource(page, "a")).toBeVisible();
  await expect(resource(page, "b")).toBeVisible();
  expect(requests.filter((u) => u.includes("/containers/"))).toHaveLength(0);
  await page.getByLabel("Buscar container ou servidor").fill("api");
  await page.getByLabel("Período", { exact: true }).selectOption("6h");
  await resource(page, "b").click();
  await expect(resource(page, "a")).toBeVisible();
  await expect(page.getByLabel("Servidor", { exact: true })).toHaveValue("");
  await expect(page.locator(".chart-reading").first()).toContainText("25");
  await page
    .locator(".entity-actions")
    .getByRole("button", { name: "Métricas", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Métricas do recurso" }),
  ).toBeVisible();
  expect(
    requests.some(
      (u) =>
        u.includes("/containers/api") && u.includes("host=cadvisor-b%3A8080"),
    ),
  ).toBeTruthy();
  expect(requests.some((u) => u.includes("/hosts/"))).toBeFalsy();
  await page
    .locator(".entity-actions")
    .getByRole("button", { name: "Registros de eventos" })
    .click();
  await expect(
    page.getByText("Escopo aplicado:", { exact: false }),
  ).toContainText("node-b");
  await page.getByRole("button", { name: "← Voltar" }).click();
  await expect(
    page.getByRole("heading", { name: "Métricas do recurso" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "← Voltar" }).click();
  await expect(page.getByLabel("Buscar container ou servidor")).toHaveValue(
    "api",
  );
  await expect(resource(page, "b")).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByLabel("Período", { exact: true })).toHaveValue("6h");
  await page.reload();
  await expect(resource(page, "b")).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByLabel("Período", { exact: true })).toHaveValue("6h");
  await page.getByRole("button", { name: "Início", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Sua operação, com contexto." }),
  ).toBeVisible();
});
test("troca A → B não permite detalhes antigos nem resposta fora de ordem", async ({
  page,
}) => {
  let releaseA: () => void = () => {};
  const pendingA = new Promise<void>((resolve) => (releaseA = resolve));
  await page.route("**/observability/containers/api?*", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("host")?.includes("-a:")) await pendingA;
    await route
      .fulfill({
        json: {
          data: detail(
            url,
            url.searchParams.get("host")?.includes("-a:") ? 11 : 88,
          ),
        },
      })
      .catch(() => {});
  });
  await openContainers(page);
  await resource(page, "a").click();
  await expect(
    page.getByText("Consultando Gráficos", { exact: false }),
  ).toBeVisible();
  await resource(page, "b").click();
  await expect(page.locator(".chart-reading").first()).toContainText("88");
  releaseA();
  await expect(page.locator(".mini-chart")).not.toContainText("11 %");
});
test("atualizar consulta a tela ativa e recupera erro sem manter detalhes anteriores", async ({
  page,
}) => {
  let count = 0;
  let failing = true;
  await page.route("**/observability/containers/api?*", async (route) => {
    count++;
    if (failing)
      await route.fulfill({
        status: 503,
        json: { error: { message: "Falha controlada", requestId: "fixture" } },
      });
    else
      await route.fulfill({
        json: { data: detail(new URL(route.request().url()), 77) },
      });
  });
  await openContainers(page);
  await resource(page, "a").click();
  await expect(page.getByRole("alert")).toContainText("Falha controlada");
  failing = false;
  await page.getByRole("button", { name: "Tentar novamente" }).click();
  await expect(page.locator(".chart-reading").first()).toContainText("77");
  await expect(page.getByRole("alert")).toHaveCount(0);
  const before = count;
  await page.getByRole("button", { name: "Atualizar dados" }).click();
  await expect.poll(() => count).toBeGreaterThan(before);
});
test("logs enviam apenas escopo confirmado e só buscam após enviar formulário", async ({
  page,
}) => {
  const requests: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("/observability/logs")) requests.push(r.url());
  });
  await page.goto(
    "/sentinelops/?page=logs&host=cadvisor-a:8080&hostName=node-a&container=api",
  );
  await expect(
    page.getByText("O container selecionado não tem", { exact: false }),
  ).toBeVisible();
  expect(requests).toHaveLength(0);
  await page
    .getByRole("button", { name: "Limpar seleção e consultar todas as fontes" })
    .click();
  await expect.poll(() => requests.length).toBeGreaterThan(0);
  const before = requests.length;
  await page.getByLabel("Texto da mensagem").fill("falha literal");
  expect(requests.length).toBe(before);
  await page.getByRole("button", { name: "Buscar eventos" }).click();
  await expect
    .poll(() => requests.some((u) => u.includes("search=falha+literal")))
    .toBeTruthy();
  await expect(
    page.getByText("Até 300 eventos", { exact: false }),
  ).toBeVisible();
});
for (const state of ["no_data", "partial", "unavailable", "old"])
  test(`fonte ${state} permanece distinta de saúde`, async ({ page }) => {
    await page.route("**/observability/hosts?*", (r) =>
      r.fulfill({
        json: {
          data: {
            items: [],
            sources: {
              prometheus: source(
                state === "old" ? "available" : state,
                state === "old",
              ),
            },
          },
        },
      }),
    );
    await page.route("**/observability/hosts", (r) =>
      r.fulfill({
        json: {
          data: {
            items: [],
            sources: {
              prometheus: source(
                state === "old" ? "available" : state,
                state === "old",
              ),
            },
          },
        },
      }),
    );
    await page.goto("/sentinelops/?page=hosts");
    await expect(page.locator(".source-banner")).toContainText(
      (
        {
          no_data: "Sem dados",
          partial: "Dados parciais",
          unavailable: "Fonte indisponível",
          old: "consulta desatualizada",
        } as Record<string, string>
      )[state],
    );
  });
test("servidor → containers usa identidade canônica sem comparar portas de exporters", async ({
  page,
}) => {
  await page.goto("/sentinelops/?page=hosts");
  await page.getByRole("button", { name: /node-a exporter-a/ }).click();
  await page.getByRole("button", { name: "Containers deste servidor" }).click();
  await expect(page.getByLabel("Servidor", { exact: true })).toHaveValue(
    "node-a",
  );
  await expect(resource(page, "a")).toBeVisible();
  await expect(resource(page, "b")).toHaveCount(0);
  await page.getByRole("button", { name: "Limpar filtros" }).click();
  await expect(resource(page, "b")).toBeVisible();
});
test("traces e perfis têm encaminhamentos reais com período", async ({
  page,
}) => {
  await page.goto("/sentinelops/?page=traces&window=6h");
  await expect(
    page.getByRole("link", { name: "Abrir detalhes no Grafana" }),
  ).toHaveAttribute("href", /queryType.*traceql/);
  await page
    .getByRole("navigation", { name: "Principal" })
    .getByRole("button", { name: "Perfis de execução" })
    .click();
  await expect(
    page.getByRole("link", { name: "Abrir ferramenta integrada" }),
  ).toHaveAttribute("href", /datasource.*pyroscope/);
  await expect(
    page.getByText(
      "A seleção de servidor/container de outras páginas não é convertida automaticamente.",
      { exact: false },
    ),
  ).toBeVisible();
});
test("mobile, teclado, temas e sair continuam acessíveis", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openContainers(page);
  await page.getByRole("button", { name: "Menu de navegação" }).click();
  await expect(
    page
      .getByRole("navigation", { name: "Principal" })
      .getByRole("button", { name: "Servidores", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Sair", exact: true }),
  ).toBeVisible();
  await resource(page, "a").focus();
  await page.keyboard.press("Enter");
  await expect(resource(page, "a")).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Alternar tema" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({ path: "artifacts/mobile-light.png", fullPage: true });
  await page.getByRole("button", { name: "Alternar tema" }).click();
  await page.screenshot({ path: "artifacts/mobile-dark.png", fullPage: true });
});
test("portal preserva oito caminhos e apresenta quatro grupos", async ({
  page,
}) => {
  const html = readFileSync(
    new URL(
      "../../deploy/vm/private-ingress/portal/index.html",
      import.meta.url,
    ),
    "utf8",
  );
  await page.route(
    `${new URL(String(test.info().project.use.baseURL)).origin}/`,
    (r) => r.fulfill({ contentType: "text/html", body: html }),
  );
  await page.goto("/");
  await expect(page.locator("section")).toHaveCount(4);
  await expect(page.locator("a")).toHaveCount(8);
  await expect(page.getByRole("link", { name: /SentinelOps/ })).toHaveAttribute(
    "href",
    "/sentinelops/",
  );
  await page.getByRole("link", { name: /SentinelOps/ }).click();
  await expect(
    page.getByRole("heading", { name: "Sua operação, com contexto." }),
  ).toBeVisible();
  await page.screenshot({
    path: "artifacts/overview-desktop.png",
    fullPage: true,
  });
  await page.getByRole("link", { name: "Portal de ferramentas" }).click();
  await expect(
    page.getByRole("heading", { name: "Do sinal à investigação." }),
  ).toBeVisible();
  await page.screenshot({
    path: "artifacts/portal-desktop.png",
    fullPage: true,
  });
});
test("busca de catálogo não troca de tela durante digitação", async ({
  page,
}) => {
  await page.goto("/sentinelops/");
  await page.getByLabel("Buscar no catálogo").fill("servidor");
  await expect(
    page.getByRole("heading", { name: "Sua operação, com contexto." }),
  ).toBeVisible();
  await page.getByLabel("Buscar no catálogo").press("Enter");
  await expect(
    page.getByRole("heading", {
      name: "Serviços e ativos sob responsabilidade",
    }),
  ).toBeVisible();
});

test("visão geral diferencia erro de acesso e recupera a consulta", async ({
  page,
}) => {
  let failing = true;
  await page.route("**/observability/overview", (route) =>
    route.fulfill(
      failing
        ? {
            status: 403,
            json: { error: { message: "denied", requestId: "fixture" } },
          }
        : {
            json: {
              data: {
                values: { hostsUp: 2 },
                sources: { prometheus: source() },
              },
            },
          },
    ),
  );
  await page.goto("/sentinelops/");
  await expect(page.getByRole("alert")).toContainText("Sem permissão");
  failing = false;
  await page.getByRole("button", { name: "Tentar novamente" }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(page.locator(".source-banner")).toContainText(
    "Dados disponíveis",
  );
});
test("login local informa credenciais inválidas e permite nova tentativa", async ({
  page,
}) => {
  await page.addInitScript(() => sessionStorage.removeItem("sentinel-token"));
  let failing = true;
  await page.route("**/auth/login", (r) =>
    r.fulfill(
      failing
        ? {
            status: 401,
            json: { error: { message: "invalid", requestId: "fixture" } },
          }
        : { json: { data: { accessToken: "fixture-only" } } },
    ),
  );
  await page.goto("/sentinelops/");
  await page.getByLabel("Usuário", { exact: true }).fill("fixture-user");
  await page.getByLabel("Senha", { exact: true }).fill("fixture-password");
  await page.getByRole("button", { name: "Entrar", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Confira seu usuário e senha",
  );
  failing = false;
  await page.getByRole("button", { name: "Entrar", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Sua operação, com contexto." }),
  ).toBeVisible();
});
test("voltar restaura rolagem mesmo quando os gráficos chegam depois", async ({
  page,
}) => {
  await page.route("**/observability/containers/api?*", async (r) => {
    await new Promise((resolve) => setTimeout(resolve, 250));
    const data = detail(new URL(r.request().url()));
    data.metrics = Object.fromEntries(
      Array.from({ length: 20 }, (_, i) => ["fixture-" + i, data.metrics.cpu]),
    );
    await r.fulfill({ json: { data } }).catch(() => {});
  });
  await page.goto(
    "/sentinelops/?page=metrics&host=cadvisor-a:8080&hostName=node-a&container=api&containerId=" +
      id,
  );
  await expect(page.locator(".mini-chart")).toHaveCount(20);
  await page.evaluate(() => window.scrollTo(0, 1200));
  await expect.poll(() => page.evaluate(() => scrollY)).toBe(1200);
  // Navigate via keyboard without Playwright scrolling the source back to its top.
  await page
    .locator(".entity-actions")
    .getByRole("button", { name: "Registros de eventos" })
    .evaluate((b: HTMLButtonElement) => b.click());
  await expect(
    page.getByRole("heading", { name: "Registros de eventos (logs)" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "← Voltar" }).click();
  await expect(page.locator(".mini-chart")).toHaveCount(20);
  await expect.poll(() => page.evaluate(() => scrollY)).toBe(1200);
});
test("sem histórico interno, voltar de métricas usa a lista correspondente", async ({
  page,
}) => {
  await page.goto(
    "/sentinelops/?page=metrics&host=cadvisor-a:8080&container=api",
  );
  await page.getByRole("button", { name: "← Voltar" }).click();
  await expect(
    page.getByRole("heading", { name: "Containers", exact: true }),
  ).toBeVisible();
});
test("APM explicita que ambiente não é filtro de traces", async ({ page }) => {
  await page.route("**/observability/apm", (r) =>
    r.fulfill({
      json: {
        data: {
          items: [
            {
              name: "same-service",
              environment: "test",
              requestsPerSecond: 1,
              errorPercent: 0,
              p95Seconds: 0.01,
              p99Seconds: 0.02,
            },
          ],
          sources: { prometheus: source() },
        },
      },
    }),
  );
  await page.goto("/sentinelops/?page=apm");
  await page.getByRole("button", { name: /same-service/ }).click();
  await expect(
    page.getByText("O filtro de ambiente não é aplicado", { exact: false }),
  ).toBeVisible();
});

test("fonte envelhece sem consultas e libera o relógio ao desmontar", async ({
  page,
}) => {
  const startedAt = new Date("2026-09-18T12:00:00Z");
  await page.clock.install({ time: startedAt });
  let fetchedAt = startedAt.toISOString();
  let telemetryRequests = 0;
  page.on("request", (request) => {
    if (request.url().includes("/observability/")) telemetryRequests++;
  });
  await page.route("**/observability/hosts", (r) =>
    r.fulfill({
      json: {
        data: {
          items: hosts,
          sources: { prometheus: { state: "available", fetchedAt } },
        },
      },
    }),
  );
  await page.goto("/sentinelops/");
  await expect(page.locator(".source-banner")).toBeVisible();
  // Observe timer lifetime after the initial page has settled. This verifies
  // cleanup when navigation removes the banner, including development StrictMode.
  await page.evaluate(() => {
    const intervals = new Set<number>();
    const set = window.setInterval.bind(window);
    const clear = window.clearInterval.bind(window);
    window.setInterval = (handler, delay, ...args) => {
      const id = set(handler, delay, ...args);
      intervals.add(id);
      return id;
    };
    window.clearInterval = (id) => {
      if (id !== undefined) intervals.delete(id);
      clear(id);
    };
    Object.assign(window, { activePresentationTimers: () => intervals.size });
  });
  await page
    .getByRole("navigation", { name: "Principal" })
    .getByRole("button", { name: "Servidores", exact: true })
    .click();
  const banner = page.locator(".source-banner");
  await expect(banner).toContainText("Dados disponíveis");
  await expect(banner).not.toContainText("consulta desatualizada");
  const before = telemetryRequests;
  await page.clock.runFor(4 * 60 * 1000);
  await expect(banner).not.toContainText("consulta desatualizada");
  await page.clock.runFor(91 * 1000);
  await expect(banner).toContainText("consulta desatualizada");
  expect(telemetryRequests).toBe(before);
  // Only an explicit refresh replaces the source timestamp and clears the age.
  fetchedAt = new Date(await page.evaluate(() => Date.now())).toISOString();
  await page.getByRole("button", { name: "Atualizar dados" }).click();
  await expect(banner).not.toContainText("consulta desatualizada");
  await expect
    .poll(() =>
      page.evaluate(() => Reflect.get(window, "activePresentationTimers")()),
    )
    .toBe(1);
  await page
    .getByRole("navigation", { name: "Principal" })
    .getByRole("button", { name: "Catálogo de serviços" })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => Reflect.get(window, "activePresentationTimers")()),
    )
    .toBe(0);
  const afterUnmount = telemetryRequests;
  await page.clock.runFor(6 * 60 * 1000);
  expect(telemetryRequests).toBe(afterUnmount);
});

test("NOC: percentuais, eixos, série oculta não mascara capacidade e legenda acessível", async ({
  page,
}) => {
  await page.route("**/observability/hosts/*?*", async (route) => {
    const url = new URL(route.request().url());
    const data = detail(url);
    const end = Number(url.searchParams.get("end"));
    const series = (value: number, labels: Record<string, string>) => ({
      labels,
      points: [
        {
          timestamp: new Date((end - 15) * 1000).toISOString(),
          value: value - 1,
        },
        { timestamp: new Date(end * 1000).toISOString(), value },
      ],
    });
    data.metrics = {
      cpuByMode: [series(65, { mode: "idle" })],
      memoryUsed: [series(45, { instance: "node-a" })],
      diskUsed: [
        series(95, { mountpoint: "/" }),
        series(20, { mountpoint: "/mnt" }),
      ],
    } as typeof data.metrics;
    await route.fulfill({ json: { data } });
  });
  await page.goto(
    "/sentinelops/?page=hosts&host=exporter-a:9100&hostName=node-a",
  );
  const cpu = page
    .locator(".noc-chart")
    .filter({ has: page.locator("header b", { hasText: "CPU em uso" }) });
  await expect(cpu.locator(".chart-reading")).toContainText("35 %");
  await expect(cpu.locator(".chart-axis").first()).toContainText("0 %");
  const disk = page
    .locator(".noc-chart")
    .filter({ has: page.locator("header b", { hasText: "Espaço em disco" }) });
  await expect(disk.locator(".usage-state")).toContainText("Uso muito alto");
  await disk.getByRole("button", { name: "/", exact: true }).click();
  await expect(disk.locator(".chart-reading")).toContainText("95 %");
  await expect(disk.locator(".usage-state")).toContainText("Uso muito alto");
  await cpu.getByRole("slider").focus();
  await page.keyboard.press("ArrowLeft");
  await expect(cpu.locator(".chart-tooltip")).toContainText("amostra às");
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: "artifacts/noc-charts-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "artifacts/noc-charts-mobile.png",
    fullPage: true,
  });
});
