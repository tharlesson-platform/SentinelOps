import { afterEach, describe, expect, it, vi } from "vitest";
import { API } from "./api";

describe("API", () => {
  it("encaminha classe HTTP e filtros exatos sem mudar a janela", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ data: { items: [] } })));
    vi.stubGlobal("fetch", fetchMock);
    await new API("token").traces({ traceStatus: "4xx", error: true, service: "api", host: "node-a", container: "web", window: "6h", end: 1800000000, limit: 100 });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "https://example.test");
    expect(Object.fromEntries(url.searchParams)).toMatchObject({ traceStatus: "4xx", error: "true", service: "api", host: "node-a", container: "web", start: "1799978400", end: "1800000000", limit: "100" });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  it("preserves the configured application subpath for API requests", async () => {
    const fetchMock = vi.fn(
      async () => new Response(JSON.stringify({ data: [] })),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new API("token", "/sentinelops").services();
    expect(fetchMock).toHaveBeenCalledWith(
      "/sentinelops/api/v1/services",
      expect.anything(),
    );
  });
  it("surfaces the request id on failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: {
                code: "forbidden",
                message: "sem acesso",
                requestId: "req-1",
              },
            }),
            { status: 403, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    await expect(new API("token").services()).rejects.toThrow("req-1");
  });

  it("uses the bounded asset search endpoint with encoded query", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ data: { items: [], query: "vm edge", limit: 25 } }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new API("token").searchAssets(" vm edge ");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/assets/search?limit=25&q=vm+edge",
      expect.anything(),
    );
  });

  it("uses the tenant-scoped data lifecycle routes", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new API("token").dataLifecycleRequests();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/data-lifecycle-requests",
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: "Bearer token" }),
      }),
    );
  });

  it("sends the explicit control-plane erasure confirmation", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: {} }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new API("token").executeDataLifecycleRequest("request-1");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/data-lifecycle-requests/request-1/execute",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ confirm: "ERASE_CONTROL_PLANE_METADATA" }),
      }),
    );
  });

  it("preserves host and container context in telemetry queries", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-14T12:00:00Z"));
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: { items: [], sources: {} } }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new API("token").logs({
      host: "easy-vm",
      containerId: "abc123",
      severity: "ERROR",
      window: "15m",
      limit: 300,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/observability/logs?start=1789386300&end=1789387200&host=easy-vm&containerId=abc123&severity=ERROR&limit=300",
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: "Bearer token" }),
      }),
    );
  });

  it("encodes the selected host in native metric detail requests", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-14T12:00:00Z"));
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: { metrics: {}, sources: {} } }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new API("token").hostDetails("host edge/01", "1h");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/observability/hosts/host%20edge%2F01?start=1789383600&end=1789387200",
      expect.anything(),
    );
  });
});

describe("mensagens de acesso e cancelamento", () => {
  afterEach(() => vi.unstubAllGlobals());
  it("diferencia credenciais inválidas de sessão expirada", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: { requestId: "fixture", message: "Unauthorized" },
            }),
            { status: 401 },
          ),
      ),
    );
    await expect(new API("").login("fixture", "fixture")).rejects.toThrow(
      "Confira seu usuário e senha",
    );
    await expect(new API("fixture").hosts()).rejects.toThrow("Sessão expirada");
  });
  it("propaga cancelamento ao consultar métricas do container com servidor e intervalo", async () => {
    const fetchMock = vi.fn(
      async () => new Response(JSON.stringify({ data: {} })),
    );
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();
    await new API("fixture", "/sentinelops").containerDetails(
      "api",
      "node:8080",
      "6h",
      1800000000,
      controller.signal,
    );
    expect(fetchMock).toHaveBeenCalledWith(
      "/sentinelops/api/v1/observability/containers/api?start=1799978400&end=1800000000&host=node%3A8080",
      expect.objectContaining({ signal: controller.signal }),
    );
  });
});
