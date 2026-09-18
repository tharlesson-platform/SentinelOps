import { useEffect, useState } from "react";
import {
  API,
  ContainerDetails,
  ContainerSummary,
  HostDetails,
  HostSummary,
  TelemetryList,
  SourceStatus,
  TelemetrySeries,
} from "./api";
import { EntityContext, Route } from "./navigation";
export type { EntityContext } from "./navigation";
export type ObservabilityPage =
  | "hosts"
  | "docker"
  | "logs"
  | "metrics"
  | "apm"
  | "traces"
  | "profiles";
type Props = {
  page: ObservabilityPage;
  api: API;
  route: Route;
  update: (value: Partial<Route>, replace?: boolean) => void;
  refresh: number;
};

// A response is displayed only under the exact request key that produced it.
function useQuery<T>(
  key: string,
  query: (signal: AbortSignal) => Promise<T>,
  enabled = true,
) {
  const [result, setResult] = useState<{
    key: string;
    data?: T;
    error?: string;
    loading: boolean;
  }>({ key: "", loading: true });
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    setResult({ key, loading: true });
    query(controller.signal)
      .then((data) => {
        if (!controller.signal.aborted)
          setResult({ key, data, loading: false });
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setResult({ key, error: String(e), loading: false });
      });
    return () => controller.abort();
  }, [key, retry, enabled]);
  return {
    ...(result.key === key
      ? result
      : { loading: enabled, data: undefined, error: undefined }),
    retry: () => setRetry((n) => n + 1),
  };
}
export function ObservabilityView(props: Props) {
  const { page, route, update } = props;
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">TQI · OBSERVABILIDADE</p>
          <h1>{titles[page]}</h1>
          <p>{descriptions[page]}</p>
        </div>
        <label className="period">
          Período compartilhado
          <select
            aria-label="Período"
            value={route.window}
            onChange={(e) =>
              update(
                { window: e.target.value, end: Math.floor(Date.now() / 1000) },
                true,
              )
            }
          >
            {["15m", "1h", "6h", "24h"].map((w) => (
              <option key={w}>{w}</option>
            ))}
          </select>
          <small>
            Até {new Date(route.end * 1000).toLocaleString("pt-BR")}
          </small>
        </label>
      </div>
      {page === "hosts" || page === "docker" ? (
        <Inventory {...props} />
      ) : page === "metrics" ? (
        <Details {...props} />
      ) : page === "logs" ? (
        <Logs {...props} />
      ) : page === "traces" ? (
        <Traces {...props} />
      ) : page === "apm" ? (
        <APM {...props} />
      ) : (
        <Profiles {...props} />
      )}
    </>
  );
}
const titles = {
  hosts: "Servidores (hosts)",
  docker: "Containers",
  metrics: "Métricas do recurso",
  logs: "Registros de eventos (logs)",
  traces: "Caminho das requisições (traces)",
  apm: "Desempenho das aplicações",
  profiles: "Perfis de execução",
};
const descriptions = {
  hosts:
    "Escolha um servidor para investigar processamento, memória, disco e rede.",
  docker:
    "Busque em todos os servidores e escolha o container pelo nome e servidor.",
  metrics: "Gráficos do recurso selecionado, com período, unidade e origem.",
  logs: "Busque mensagens para entender o que aconteceu.",
  traces: "Investigue por onde uma requisição passou e onde demorou.",
  apm: "Taxa de requisições, erros e tempo de resposta. Janela fixa dos últimos 5 minutos; o período compartilhado não se aplica a estes indicadores.",
  profiles: "Descubra onde a aplicação gasta processamento e memória.",
};
function Inventory(props: Props) {
  const { api, page, route, update, refresh } = props;
  const docker = page === "docker";
  const inventory = useQuery<TelemetryList<HostSummary | ContainerSummary>>(
    `${page}:${refresh}`,
    (signal) => (docker ? api.containers({}, signal) : api.hosts("", signal)),
  );
  const items = inventory.data?.items || [];
  const hosts = [
    ...new Set(
      items.map((i) =>
        "host" in i ? i.hostName || i.host : i.hostName || i.instance,
      ),
    ),
  ];
  const envs = [
    ...new Set(items.map((i) => i.environment).filter(Boolean)),
  ] as string[];
  const shown = items.filter(
    (i) =>
      JSON.stringify(i).toLowerCase().includes(route.query.toLowerCase()) &&
      (!route.environment || i.environment === route.environment) &&
      (!route.hostFilter ||
        ("host" in i ? i.hostName || i.host : i.hostName || i.instance) ===
          route.hostFilter),
  );
  const selected = (i: HostSummary | ContainerSummary) =>
    "host" in i
      ? route.context.container === i.name && route.context.host === i.host
      : !route.context.container && route.context.host === i.instance;
  function choose(i: HostSummary | ContainerSummary) {
    update({
      context:
        "host" in i
          ? {
              host: i.host,
              hostName: i.hostName,
              container: i.name,
              containerId: i.logContainerId,
            }
          : { host: i.instance, hostName: i.hostName },
    });
  }
  return (
    <>
      <QueryStatus
        query={inventory}
        source={inventory.data?.sources.prometheus}
        label="Inventário · Prometheus"
      />
      <div className="inventory-filters">
        <label>
          Buscar {docker ? "container ou servidor" : "servidor"}
          <input
            type="search"
            value={route.query}
            onChange={(e) => update({ query: e.target.value }, true)}
          />
        </label>
        <label>
          Servidor
          <select
            aria-label="Servidor"
            value={route.hostFilter}
            onChange={(e) => update({ hostFilter: e.target.value }, true)}
          >
            <option value="">Todos os servidores</option>
            {route.hostFilter && !hosts.includes(route.hostFilter) && (
              <option>{route.hostFilter}</option>
            )}
            {hosts.map((h) => (
              <option key={h}>{h}</option>
            ))}
          </select>
        </label>
        <label>
          Ambiente
          <select
            aria-label="Ambiente"
            value={route.environment}
            onChange={(e) => update({ environment: e.target.value }, true)}
          >
            <option value="">Todos os ambientes</option>
            {route.environment && !envs.includes(route.environment) && (
              <option>{route.environment}</option>
            )}
            {envs.map((e) => (
              <option key={e}>{e}</option>
            ))}
          </select>
        </label>
        <button
          onClick={() =>
            update({ query: "", hostFilter: "", environment: "" }, true)
          }
        >
          Limpar filtros
        </button>
      </div>
      <div className="obs-layout">
        <section
          className="obs-list"
          aria-label={docker ? "Escolha um container" : "Escolha um servidor"}
        >
          <div className="obs-list-head">
            <b>{shown.length} recursos</b>
            <span>Seleção explícita</span>
          </div>
          {shown.map((i) => (
            <button
              key={"host" in i ? `${i.host}/${i.name}` : i.instance}
              aria-pressed={selected(i)}
              className={selected(i) ? "selected" : ""}
              onClick={() => choose(i)}
            >
              <span aria-hidden="true">{"host" in i ? "▣" : "▤"}</span>
              <div>
                <strong>{i.name}</strong>
                <small>
                  {"host" in i ? i.host : i.instance} ·{" "}
                  {i.environment || "Ambiente não informado"}
                </small>
                <small>{stateLabel(i.state)}</small>
              </div>
              <span>
                {i.cpuPercent == null
                  ? "Sem dados"
                  : `${i.cpuPercent.toFixed(1)}%${docker ? " de 1 núcleo" : ""}`}
              </span>
            </button>
          ))}
          {!inventory.loading && !inventory.error && !shown.length && (
            <NoData
              text={
                inventory.data?.sources.prometheus.state === "unavailable"
                  ? "Inventário indisponível. Tente novamente; ausência de resposta não significa ausência de recursos."
                  : "Nenhum resultado. Limpe os filtros ou verifique a fonte de coleta."
              }
            />
          )}
        </section>
        <section className="obs-detail">
          {route.context.host &&
          (docker ? !!route.context.container : !route.context.container) ? (
            <Details {...props} />
          ) : (
            <NoData
              text={`Selecione ${docker ? "um container identificado pelo servidor" : "um servidor"} na lista para abrir seus gráficos.`}
            />
          )}
        </section>
      </div>
    </>
  );
}
function Details(props: Props) {
  const { api, route, update, refresh } = props;
  const c = route.context;
  const key = JSON.stringify([
    c.host,
    c.container,
    route.window,
    route.end,
    refresh,
  ]);
  const result = useQuery<HostDetails | ContainerDetails>(
    key,
    (signal) =>
      c.container
        ? api.containerDetails(
            c.container,
            c.host,
            route.window,
            route.end,
            signal,
          )
        : api.hostDetails(c.host!, route.window, route.end, signal),
    !!c.host,
  );
  if (!c.host)
    return (
      <>
        <NoData text="Escolha um servidor ou container antes de abrir métricas." />
        <button onClick={() => update({ page: "hosts" })}>
          Escolher servidor
        </button>
        <button onClick={() => update({ page: "docker" })}>
          Escolher container
        </button>
      </>
    );
  return (
    <>
      <div className="entity-head">
        <div>
          <p className="eyebrow">{c.container ? "CONTAINER" : "SERVIDOR"}</p>
          <h2>{c.container || c.host}</h2>
          <p>
            {c.container
              ? `Servidor de métricas: ${c.host}`
              : "Processamento, memória, disco e rede"}
          </p>
        </div>
      </div>
      <div className="entity-actions">
        <button onClick={() => update({ page: "metrics" })}>Métricas</button>
        <button onClick={() => update({ page: "logs" })}>
          Registros de eventos
        </button>
        <button onClick={() => update({ page: "traces" })}>Requisições</button>
        {!c.container && (
          <button
            disabled={!c.hostName}
            title={
              !c.hostName
                ? "A coleta ainda não fornece host_name para correlacionar exporters"
                : undefined
            }
            onClick={() =>
              update({ page: "docker", hostFilter: c.hostName!, query: "" })
            }
          >
            Containers deste servidor
          </button>
        )}
      </div>
      {!c.hostName && (
        <p className="notice">
          Correlação com outras fontes ainda não informada pela coleta
          (host_name). Métricas continuam disponíveis.
        </p>
      )}
      <QueryStatus
        query={result}
        source={result.data?.sources.prometheus}
        label="Gráficos · Prometheus"
      />
      {result.data && (
        <>
          <p className="measurement-window">
            {new Date(result.data.start).toLocaleString("pt-BR")} —{" "}
            {new Date(result.data.end).toLocaleString("pt-BR")} · amostras a
            cada {result.data.stepSeconds}s
          </p>
          <div className="chart-grid">
            {Object.entries(result.data.metrics).map(([name, series]) => (
              <MiniChart
                key={name}
                name={name}
                series={series || []}
                start={result.data!.start}
                end={result.data!.end}
                step={result.data!.stepSeconds}
              />
            ))}
          </div>
          {Object.keys(result.data.metrics).length === 0 && (
            <NoData text="Sem telemetria para este recurso no período. Verifique a coleta ou altere o período." />
          )}
        </>
      )}
    </>
  );
}
function appliedScope(c: EntityContext, kind: "logs" | "traces") {
  if (c.host && !c.hostName)
    return "O servidor selecionado não tem correlação host_name. Nenhuma busca foi executada.";
  if (kind === "logs" && c.container && !c.containerId)
    return "O container selecionado não tem um ID de log confirmado. Nenhuma busca foi executada.";
  return "";
}
function Scope({
  context,
  kind,
  clear,
}: {
  context: EntityContext;
  kind: "logs" | "traces";
  clear: () => void;
}) {
  const blocked = appliedScope(context, kind);
  return (
    <>
      <div className="context-strip">
        <span>
          Escopo aplicado:{" "}
          {blocked
            ? "nenhum"
            : context.hostName || context.service
              ? ""
              : "fontes identificadas por serviço ou servidor"}
          {!blocked && (
            <>
              {context.hostName && `servidor ${context.hostName}`}
              {context.container &&
                ` · container ${kind === "logs" ? context.containerId : context.container}`}
              {context.service && ` · serviço ${context.service}`}
            </>
          )}
        </span>
        <button onClick={clear}>
          Limpar seleção e consultar todas as fontes
        </button>
      </div>
      {context.serviceEnvironment && (
        <p className="notice">
          Ambiente de origem: {context.serviceEnvironment}. O filtro de ambiente
          não é aplicado nesta busca; resultados podem incluir outros ambientes
          com o mesmo serviço e servidor.
        </p>
      )}
      {blocked && (
        <div className="notice" role="status">
          {blocked} Volte à lista para escolher outro recurso ou limpe a seleção
          explicitamente.
        </div>
      )}
    </>
  );
}
function Logs({ api, route, update, refresh }: Props) {
  const [draft, setDraft] = useState(route.logSearch);
  const [severity, setSeverity] = useState(route.severity);
  const c = route.context;
  useEffect(() => {
    setDraft(route.logSearch);
    setSeverity(route.severity);
  }, [route.logSearch, route.severity]);
  const blocked = appliedScope(c, "logs");
  const result = useQuery(
    JSON.stringify([
      "logs",
      c,
      route.window,
      route.end,
      route.logSearch,
      route.severity,
      refresh,
    ]),
    (signal) =>
      api.logs(
        {
          host: c.hostName,
          containerId: c.containerId,
          service: c.service,
          search: route.logSearch,
          severity: route.severity,
          window: route.window,
          end: route.end,
          limit: 300,
        },
        signal,
      ),
    !blocked,
  );
  return (
    <>
      <Scope context={c} kind="logs" clear={() => update({ context: {} })} />
      <form
        className="querybar"
        onSubmit={(e) => {
          e.preventDefault();
          update({ logSearch: draft, severity }, true);
          result.retry();
        }}
      >
        <label>
          Texto da mensagem
          <input value={draft} onChange={(e) => setDraft(e.target.value)} />
        </label>
        <label>
          Severidade (busca no texto)
          <select
            value={severity}
            onChange={(e) => setSeverity(e.target.value)}
          >
            <option value="">Todas</option>
            {["ERROR", "WARN", "INFO", "DEBUG"].map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
        </label>
        <button disabled={!!blocked}>Buscar eventos</button>
      </form>
      {!blocked && (
        <>
          <QueryStatus
            query={result}
            source={result.data?.sources.loki}
            label="Eventos · Loki"
          />
          <p>
            Até 300 eventos mais recentes na janela.{" "}
            {result.data?.items.length === 300
              ? "Limite atingido; refine o texto ou reduza o período para investigar sem omissões."
              : "Para continuar a investigação, refine os filtros ou use o Grafana."}
          </p>
          <a
            className="link-button"
            href={exploreURL(
              "loki",
              route,
              result.data?.queries || result.data?.query,
            )}
          >
            Investigar no Grafana
          </a>
          <div className="log-table">
            {result.data?.items.map((item, i) => (
              <article key={`${item.timestamp}-${i}`}>
                <time>{new Date(item.timestamp).toLocaleString("pt-BR")}</time>
                <span>{item.labels.level || "Evento"}</span>
                <div>
                  <b>
                    {item.labels.container_id ||
                      item.labels.host_name ||
                      item.labels.service_name ||
                      "Origem não informada"}
                  </b>
                  <pre>{item.line}</pre>
                </div>
              </article>
            ))}
          </div>
          {result.data &&
            !result.data.items.length &&
            result.data.sources.loki.state !== "unavailable" && (
              <NoData text="Nenhum evento encontrado com estes filtros e período." />
            )}
        </>
      )}
    </>
  );
}
function Traces({ api, route, update, refresh }: Props) {
  const c = route.context;
  const blocked = appliedScope(c, "traces");
  const result = useQuery(
    JSON.stringify([
      "traces",
      c,
      route.window,
      route.end,
      route.errors,
      refresh,
    ]),
    (signal) =>
      api.traces(
        {
          host: c.hostName,
          container: c.container,
          service: c.service,
          error: route.errors,
          window: route.window,
          end: route.end,
          limit: 100,
        },
        signal,
      ),
    !blocked,
  );
  return (
    <>
      <Scope context={c} kind="traces" clear={() => update({ context: {} })} />
      <p>
        Filtros por atributos OpenTelemetry: a aplicação precisa emitir
        host.name, container.name e service.name correspondentes. Até 100
        requisições; refine o período se atingir o limite.
      </p>
      <label className="check">
        <input
          type="checkbox"
          checked={route.errors}
          onChange={(e) => update({ errors: e.target.checked }, true)}
        />
        Somente erros
      </label>
      {!blocked && (
        <>
          <QueryStatus
            query={result}
            source={result.data?.sources.tempo}
            label="Requisições · Tempo"
          />
          <div className="trace-list">
            {result.data?.items.map((item) => (
              <article key={item.traceId}>
                <div>
                  <b>{item.rootServiceName || "Serviço não informado"}</b>
                  <span>{item.rootTraceName}</span>
                  <code>{item.traceId}</code>
                </div>
                <strong>{item.durationMs.toFixed(1)} ms</strong>
                <a href={exploreURL("tempo", route, item.traceId)}>
                  Abrir detalhes no Grafana
                </a>
              </article>
            ))}
          </div>
          {result.data &&
            !result.data.items.length &&
            result.data.sources.tempo.state !== "unavailable" && (
              <NoData text="Nenhuma requisição encontrada. Verifique o período, os filtros e a instrumentação." />
            )}
        </>
      )}
    </>
  );
}
function APM({ api, route, update, refresh }: Props) {
  const result = useQuery(`apm:${refresh}`, (signal) => api.apm(signal));
  return (
    <>
      <QueryStatus
        query={result}
        source={result.data?.sources.prometheus}
        label="Aplicações · Prometheus"
      />
      <div className="apm-table">
        <div className="apm-row header">
          <span>Aplicação</span>
          <span>Requisições/s</span>
          <span>Erros</span>
          <span>95% abaixo de</span>
          <span>99% abaixo de</span>
        </div>
        {result.data?.items.map((i) => (
          <button
            className="apm-row"
            key={`${i.name}/${i.environment}/${i.host}`}
            onClick={() =>
              update({
                page: "traces",
                context: {
                  service: i.name,
                  hostName: i.host,
                  serviceEnvironment: i.environment,
                },
              })
            }
          >
            <span>
              <b>{i.name}</b>
              <small>
                {i.environment || "Ambiente não informado"} ·{" "}
                {i.host || "Servidor não informado"}
              </small>
            </span>
            <span>{number(i.requestsPerSecond)}</span>
            <span>{number(i.errorPercent)}%</span>
            <span>
              {number(i.p95Seconds == null ? null : i.p95Seconds * 1000)} ms
            </span>
            <span>
              {number(i.p99Seconds == null ? null : i.p99Seconds * 1000)} ms
            </span>
          </button>
        ))}
      </div>
      {result.data && !result.data.items.length && (
        <NoData text="Sem métricas de aplicação. Verifique a instrumentação OpenTelemetry." />
      )}
      <p>
        Ao abrir uma aplicação, a busca de requisições usará o período
        compartilhado de {route.window}.
      </p>
    </>
  );
}
function Profiles({ route }: Props) {
  return (
    <section className="panel">
      <h2>Investigar perfis no Grafana</h2>
      <p>
        A consulta e o gráfico de perfis estão disponíveis na ferramenta
        integrada Pyroscope. O período será preservado. Selecione a aplicação e
        o tipo de perfil lá; servidor e container não são aplicados
        automaticamente.
      </p>
      <a className="link-button" href={exploreURL("pyroscope", route)}>
        Abrir perfis de execução
      </a>
      <p>O acesso depende de sua sessão e permissão no Grafana.</p>
    </section>
  );
}
export function exploreURL(
  source: "loki" | "tempo" | "pyroscope",
  route: Pick<Route, "window" | "end">,
  expression?: string | string[],
) {
  const seconds =
    (
      { "15m": 900, "1h": 3600, "6h": 21600, "24h": 86400 } as Record<
        string,
        number
      >
    )[route.window] || 3600;
  const type = source === "pyroscope" ? "grafana-pyroscope-datasource" : source;
  const queries = (
    Array.isArray(expression) ? expression : expression ? [expression] : []
  ).map((expression, index) => ({
    refId: String.fromCharCode(65 + index),
    datasource: { type, uid: source },
    ...(source === "tempo"
      ? { query: expression, queryType: "traceql" }
      : { expr: expression }),
  }));
  return `/grafana/explore?${new URLSearchParams({ schemaVersion: "1", orgId: "1", panes: JSON.stringify({ investigation: { datasource: source, queries, range: { from: String((route.end - seconds) * 1000), to: String(route.end * 1000) } } }) })}`;
}
function number(n: number | null | undefined) {
  return n == null
    ? "Sem dados"
    : n.toLocaleString("pt-BR", { maximumFractionDigits: 2 });
}
function stateLabel(state: string) {
  return (
    (
      {
        up: "Coleta respondendo",
        down: "Coleta sem resposta",
        running: "Observado recentemente",
        stale: "Última observação antiga",
        telemetry_unknown: "Estado da coleta desconhecido",
      } as Record<string, string>
    )[state] || state
  );
}
export function SourceBanner({
  source,
  label = "Fonte",
}: {
  source?: SourceStatus;
  label?: string;
}) {
  const [now, setNow] = useState(Date.now);
  const fetchedAt = source?.fetchedAt;
  useEffect(() => {
    if (!fetchedAt) return;
    // Presentation clock only: aging must never trigger a telemetry query.
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, [fetchedAt]);
  if (!source) return null;
  const stale = now - Date.parse(source.fetchedAt) > 5 * 60 * 1000;
  return (
    <div
      className={`source-banner ${stale && source.state === "available" ? "partial" : source.state}`}
      role="status"
    >
      <div>
        <b>
          {label} ·{" "}
          {
            {
              available: "Dados disponíveis",
              partial: "Dados parciais",
              no_data: "Sem dados",
              unavailable: "Fonte indisponível",
            }[source.state]
          }
          {stale ? " · consulta desatualizada" : ""}
        </b>
        <span>{source.message}</span>
        <span>
          Consulta: {new Date(source.fetchedAt).toLocaleString("pt-BR")}
        </span>
      </div>
    </div>
  );
}
function QueryStatus({
  query,
  source,
  label,
}: {
  query: { loading: boolean; error?: string; retry: () => void };
  source?: SourceStatus;
  label: string;
}) {
  return (
    <>
      {query.loading ? (
        <div role="status" className="source-banner">
          Consultando {label}…
        </div>
      ) : query.error ? (
        <div className="error" role="alert">
          {query.error}
          <button onClick={query.retry}>Tentar novamente</button>
        </div>
      ) : (
        <>
          <SourceBanner source={source} label={label} />
          {source?.state === "unavailable" || source?.state === "partial" ? (
            <button onClick={query.retry}>Tentar novamente</button>
          ) : null}
        </>
      )}
    </>
  );
}
function NoData({ text }: { text: string }) {
  return (
    <div className="empty obs-empty">
      <p>{text}</p>
    </div>
  );
}
const chartMeta: Record<string, [string, string, string]> = {
  cpuByMode: [
    "Processamento por modo",
    "%",
    "Média entre núcleos; idle representa tempo ocioso.",
  ],
  cpu: [
    "Processamento do container",
    "% de 1 núcleo",
    "100% equivale a um núcleo; pode ultrapassar 100%.",
  ],
  throttling: [
    "CPU limitada",
    "% de tempo",
    "Tempo em que o limite de CPU impediu execução.",
  ],
  memory: ["Memória utilizada", "bytes", "Conjunto de trabalho em memória."],
  memoryLimit: [
    "Limite de memória",
    "bytes",
    "Zero pode indicar ausência de limite configurado.",
  ],
  memoryUsed: [
    "Memória utilizada",
    "%",
    "Proporção da memória total do servidor.",
  ],
  swapUsed: ["Memória swap", "%", "Proporção de swap utilizada."],
  diskUsed: ["Espaço em disco", "%", "Uma série por sistema de arquivos."],
  diskRead: ["Leitura de disco", "bytes/s", "Dados lidos por segundo."],
  diskWrite: ["Escrita de disco", "bytes/s", "Dados gravados por segundo."],
  networkRx: ["Rede recebida", "bytes/s", "Tráfego de entrada por interface."],
  networkTx: ["Rede enviada", "bytes/s", "Tráfego de saída por interface."],
  filesystem: [
    "Entrada e saída em disco",
    "bytes/s",
    "Leitura e escrita do container.",
  ],
  load1: [
    "Fila de processamento · 1 min",
    "tarefas",
    "Média de tarefas em execução ou aguardando.",
  ],
  load5: [
    "Fila de processamento · 5 min",
    "tarefas",
    "Compare com a quantidade de núcleos.",
  ],
  load15: [
    "Fila de processamento · 15 min",
    "tarefas",
    "Compare com a quantidade de núcleos.",
  ],
};
const colors = [
  "#3781b0",
  "#d14555",
  "#9574cc",
  "#a07816",
  "#009786",
  "#70828f",
];
export function chartSegments(
  series: TelemetrySeries,
  start: number,
  end: number,
  max: number,
  step: number,
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
      `${(((t - start) / Math.max(end - start, 1)) * 480).toFixed(2)},${(100 - (p.value / Math.max(max, 1)) * 90).toFixed(2)}`,
    );
    previous = t;
  }
  if (segment.length) segments.push(segment);
  return segments;
}
export function MiniChart({
  name,
  series,
  start,
  end,
  step,
}: {
  name: string;
  series: TelemetrySeries[];
  start: string;
  end: string;
  step: number;
}) {
  const [label, unit, description] = chartMeta[name] || [
    name,
    "valor",
    "Série informada pela fonte.",
  ];
  const max = Math.max(
    1,
    ...series
      .flatMap((s) => s.points.map((p) => p.value ?? 0))
      .filter(Number.isFinite),
  );
  return (
    <article className="mini-chart">
      <header>
        <b>{label}</b>
        <span>
          {unit} · escala 0–{number(max)}
        </span>
      </header>
      <p>{description}</p>
      {series.some((s) =>
        s.points.some((p) => p.value != null && Number.isFinite(p.value)),
      ) ? (
        <svg
          viewBox="0 0 480 110"
          role="img"
          aria-label={`${label}: séries separadas no tempo, valores na tabela abaixo`}
        >
          {series.map((s, i) =>
            chartSegments(s, Date.parse(start), Date.parse(end), max, step).map(
              (segment, j) => (
                <g key={`${i}-${j}`}>
                  <polyline
                    style={{ stroke: colors[i % colors.length] }}
                    points={segment.join(" ")}
                  />
                  {segment.map((point, k) => (
                    <circle
                      key={k}
                      cx={point.split(",")[0]}
                      cy={point.split(",")[1]}
                      r="1.5"
                      fill={colors[i % colors.length]}
                    />
                  ))}
                </g>
              ),
            ),
          )}
        </svg>
      ) : (
        <div className="chart-no-data">Sem amostras no período</div>
      )}
      <footer>
        <time>{new Date(start).toLocaleTimeString("pt-BR")}</time>
        <time>{new Date(end).toLocaleTimeString("pt-BR")}</time>
      </footer>
      <ul className="series-legend">
        {series.map((s, i) => {
          const last = [...s.points]
            .filter((p) => p.value != null && Number.isFinite(p.value))
            .sort(
              (a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp),
            )[0];
          const name =
            Object.entries(s.labels)
              .filter(([key]) => key !== "__name__")
              .map(([k, v]) => `${k}=${v}`)
              .join(" · ") || "Série única";
          return (
            <li key={i}>
              <i style={{ background: colors[i % colors.length] }} />
              {name}
              <br />
              {last
                ? `Última amostra: ${number(last.value)} ${unit} às ${new Date(last.timestamp).toLocaleTimeString("pt-BR")}${Date.parse(end) - Date.parse(last.timestamp) > step * 2000 ? " · amostra antiga" : ""}`
                : "Sem amostras"}
            </li>
          );
        })}
      </ul>
      <details>
        <summary>Ver dados em tabela</summary>
        <div className="chart-data">
          <table>
            <thead>
              <tr>
                <th>Série</th>
                <th>Horário</th>
                <th>Valor ({unit})</th>
              </tr>
            </thead>
            <tbody>
              {series.flatMap((s, i) =>
                s.points.map((p, j) => (
                  <tr key={`${i}-${j}`}>
                    <td>
                      {Object.values(s.labels).join(" · ") || "Série única"}
                    </td>
                    <td>{new Date(p.timestamp).toLocaleString("pt-BR")}</td>
                    <td>{number(p.value)}</td>
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
