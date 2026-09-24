import { formatMeasurement } from "./chartModel";
import { useEffect, useState } from "react";
import {
  API,
  LogEntry,
  SourceStatus,
  TelemetrySeries,
  TraceSummary,
  telemetryWindow,
} from "./api";
import { Route } from "./navigation";
import {
  MiniChart,
  QueryStatus,
  SourceBanner,
  exploreURL,
  useQuery,
} from "./Observability";

type Props = {
  api: API;
  route: Route;
  update: (v: Partial<Route>, replace?: boolean) => void;
  refresh: number;
};
type Metric = {
  name: string;
  type: string;
  unit: string;
  help: string;
  domain: string;
};
type Variable = {
  name: string;
  label: string;
  type: string;
  query: unknown;
  includeAll: boolean;
  current?: { value?: string };
  allValue?: string;
};
type Panel = {
  id: number;
  title: string;
  type: string;
  description?: string;
  datasource: { type: string; uid: string };
  targets: { refId: string; expr?: string; query?: string }[];
  fieldConfig: { defaults: { unit: string } };
  options: { content?: string; mode?: string };
};
type Dashboard = {
  uid: string;
  title: string;
  panels: Panel[];
  templating: { list: Variable[] };
};
type Catalog = {
  metrics: Metric[];
  dashboards: Dashboard[];
  panelCount: number;
  sources: Record<string, SourceStatus>;
  truncated: boolean;
  catalogNote: string;
};
type QueryResult = {
  type: string;
  series: TelemetrySeries[];
  logs?: LogEntry[];
  warnings?: string[];
  truncated: boolean;
  nonfinite: number;
};
type MetricResult = {
  result: QueryResult;
  query: string;
  filters: Record<string, string>;
  source: SourceStatus;
  start: string;
  end: string;
  stepSeconds: number;
};
type PanelResult = {
  panel: Panel;
  targets: {
    refId: string;
    sourceName: string;
    legendFormat?: string;
    query: string;
    appliedFilters: Record<string, string>;
    result?: QueryResult;
    traces?: TraceSummary[];
    source: SourceStatus;
  }[];
  start: string;
  end: string;
  stepSeconds: number;
  note: string;
};
export function readSelection(raw?: string): Record<string, string> {
  try {
    const o = JSON.parse(raw || "{}");
    return o && typeof o === "object" && !Array.isArray(o)
      ? (Object.fromEntries(
          Object.entries(o).filter(([, v]) => typeof v === "string"),
        ) as Record<string, string>)
      : {};
  } catch {
    return {};
  }
}
export function defaultVariables(dashboard: Dashboard): Record<string, string> {
  return Object.fromEntries(
    dashboard.templating.list.map((v) => {
      let value = v.current?.value || "__all__";
      if (value === "$__all") value = "__all__";
      if (v.type === "textbox" && value === ".*") value = "";
      return [v.name, value];
    }),
  );
}
function params(route: Route, extra: Record<string, string> = {}) {
  const p = telemetryWindow(route.window, route.end);
  for (const [k, v] of Object.entries(extra)) if (v) p.set(k, v);
  return p;
}
function request<T>(
  api: API,
  path: string,
  route: Route,
  extra: Record<string, string>,
  signal?: AbortSignal,
) {
  return api.request<T>(
    `/api/v1/observability/explorer/${path}?${params(route, extra)}`,
    { signal },
  );
}
function label(k: string) {
  return (
    (
      {
        job: "Job de coleta",
        instance: "Instância da fonte",
        host_name: "Servidor identificado pela coleta",
        deployment_environment: "Ambiente",
        service_name: "Serviço",
        name: "Nome do container",
        application: "Aplicação / job",
        host: "Servidor da fonte",
        container: "Container",
        environment: "Ambiente",
        route: "Rota HTTP",
        search: "Texto literal (vazio: sem filtro)",
        metric: "Métrica",
        container_id: "ID emitido pela fonte",
        endpoint: "Endpoint VMware",
        vm: "Máquina virtual",
        datastore: "Datastore",
        device: "Dispositivo",
        mountpoint: "Ponto de montagem",
        severity: "Severidade",
        alertname: "Alerta",
        stream: "Fluxo",
      } as Record<string, string>
    )[k] || k
  );
}
export function displayUnit(unit: string) {
  return (
    (
      {
        percent: "% (0–100)",
        percentunit: "fração (0–1)",
        short: "valor",
        s: "segundos",
        reqps: "requisições/s",
        Bps: "bytes/s",
        bytes: "bytes",
      } as Record<string, string>
    )[unit] ||
    unit ||
    "unidade não informada"
  );
}
function labelsText(labels: Record<string, string>) {
  return (
    Object.entries(labels)
      .map(([k, v]) => `${k}=${v}`)
      .join(" · ") || "Série única"
  );
}
export function lastFinite(series: TelemetrySeries) {
  return [...series.points]
    .filter((p) => Number.isFinite(p.value) && p.value != null)
    .sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp))[0];
}

export function Explorer(props: Props) {
  const { api, route, refresh, update } = props;
  const catalog = useQuery<Catalog>(
    `catalog:${route.window}:${route.end}:${refresh}`,
    (s) => request(api, "catalog", route, {}, s),
  );
  const mode = route.explorerMode || "views";
  return (
    <>
      <nav className="explorer-tabs" aria-label="Explorar observabilidade">
        {[
          ["views", "Visões e painéis"],
          ["metrics", "Catálogo de métricas"],
          ["sources", "Alvos e regras"],
        ].map(([id, text]) => (
          <button
            key={id}
            aria-pressed={mode === id}
            onClick={() => update({ explorerMode: id })}
          >
            {text}
          </button>
        ))}
      </nav>
      <QueryStatus
        query={catalog}
        label="Descoberta · Prometheus"
        source={catalog.data?.sources.prometheus_names}
      />
      {catalog.data && (
        <SourceBanner
          label="Metadados de tipo e unidade"
          source={catalog.data.sources.prometheus_metadata}
        />
      )}
      {catalog.data && (
        <p className="notice">
          {catalog.data.metrics.length} nomes de métricas na janela ·{" "}
          {catalog.data.dashboards.length} visões · {catalog.data.panelCount}{" "}
          painéis configurados. A consulta de cada sinal confirma se existem
          amostras.{" "}
          {catalog.data.truncated && "Índice limitado: cobertura incompleta."}
        </p>
      )}
      {mode === "sources" ? (
        <Platform {...props} />
      ) : catalog.data ? (
        mode === "metrics" ? (
          <Metrics {...props} catalog={catalog.data} />
        ) : (
          <Views {...props} catalog={catalog.data} />
        )
      ) : null}
    </>
  );
}
function Metrics({
  api,
  route,
  refresh,
  update,
  catalog,
}: Props & { catalog: Catalog }) {
  const [search, setSearch] = useState("");
  const [domain, setDomain] = useState("");
  const [page, setPage] = useState(0);
  const filters = readSelection(route.filters);
  const metric = catalog.metrics.find((m) => m.name === route.metric);
  const metricKey = metric?.name || "";
  const visible = catalog.metrics.filter(
    (m) =>
      (!domain || m.domain === domain) &&
      `${m.name} ${m.help}`.toLowerCase().includes(search.toLowerCase()),
  );
  const dimensions = useQuery<{
    labels: Record<string, string[]>;
    seriesCount: number;
    truncated: boolean;
    source: SourceStatus;
  }>(
    `dimensions:${metricKey}:${route.filters}:${route.window}:${route.end}:${refresh}`,
    (s) =>
      request(
        api,
        "dimensions",
        route,
        { metric: metricKey, filters: JSON.stringify(filters) },
        s,
      ),
    !!metric,
  );
  const [submitted, setSubmitted] = useState("");
  const key = JSON.stringify([
    metricKey,
    route.filters,
    route.operation,
    route.window,
    route.end,
    refresh,
  ]);
  // Navigation/period/refresh updates requery an already chosen metric; typing a filter requires explicit consultation.
  const result = useQuery<MetricResult>(
    key,
    (s) =>
      request(
        api,
        "metric",
        route,
        {
          metric: metricKey,
          filters: JSON.stringify(filters),
          operation: route.operation || "raw",
        },
        s,
      ),
    !!metric && submitted === key,
  );
  useEffect(() => {
    if (metric) setSubmitted(key);
  }, [metricKey, route.window, route.end, refresh]);
  const context = route.context;
  const contextFits =
    !!context.host &&
    !!dimensions.data?.labels.instance?.includes(context.host) &&
    (!context.container ||
      !!dimensions.data?.labels.name?.includes(context.container));
  return (
    <>
      <div className="inventory-filters">
        <label>
          Pesquisar métrica ou descrição
          <input
            type="search"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(0);
            }}
          />
        </label>
        <label>
          Domínio
          <select
            value={domain}
            onChange={(e) => {
              setDomain(e.target.value);
              setPage(0);
            }}
          >
            <option value="">Todos os domínios</option>
            {[...new Set(catalog.metrics.map((m) => m.domain))]
              .sort()
              .map((x) => (
                <option key={x}>{x}</option>
              ))}
          </select>
        </label>
      </div>
      <div className="explorer-layout">
        <section aria-label="Escolha uma métrica" className="explorer-list">
          <p>
            {visible.length} métricas encontradas. Página {page + 1} de{" "}
            {Math.max(1, Math.ceil(visible.length / 40))}.
          </p>
          {visible.slice(page * 40, page * 40 + 40).map((m) => (
            <button
              className="metric-choice"
              aria-pressed={metricKey === m.name}
              key={m.name}
              onClick={() =>
                update({ metric: m.name, filters: "", operation: "raw" })
              }
            >
              <b>{m.name}</b>
              <small>
                {m.domain} · {m.type || "tipo desconhecido"}
              </small>
              <span>{m.help || "Descrição não fornecida pela fonte"}</span>
            </button>
          ))}
          <div className="entity-actions">
            <button disabled={page === 0} onClick={() => setPage((p) => p - 1)}>
              Página anterior
            </button>
            <button
              disabled={(page + 1) * 40 >= visible.length}
              onClick={() => setPage((p) => p + 1)}
            >
              Próxima página
            </button>
          </div>
        </section>
        <section aria-label="Consulta da métrica" className="explorer-detail">
          {!metric ? (
            <p className="empty">
              Escolha uma métrica para descobrir seus labels e consultar séries
              reais.
            </p>
          ) : (
            <>
              <h2>{metric.name}</h2>
              <p>{metric.help || "Sem descrição na fonte."}</p>
              <p>
                Tipo: {metric.type || "desconhecido"} · unidade:{" "}
                {displayUnit(metric.unit)}.{" "}
                {metric.unit
                  ? ""
                  : "A unidade não será inventada a partir do nome."}
              </p>
              {context.host && (
                <div className="notice">
                  Contexto mantido: {context.container || context.host}. A
                  consulta utiliza somente os labels escolhidos abaixo.{" "}
                  <button
                    disabled={!contextFits}
                    onClick={() =>
                      update(
                        {
                          filters: JSON.stringify({
                            ...filters,
                            instance: context.host!,
                            ...(context.container
                              ? { name: context.container }
                              : {}),
                          }),
                        },
                        true,
                      )
                    }
                  >
                    Aplicar identidade exata da seleção
                  </button>
                  {!contextFits && (
                    <p>
                      A identidade selecionada não foi encontrada nesses labels;
                      nenhuma equivalência entre exporters foi presumida.
                    </p>
                  )}
                </div>
              )}
              <QueryStatus
                query={dimensions}
                label="Dimensões disponíveis"
                source={dimensions.data?.source}
              />
              {dimensions.data?.truncated && (
                <p className="notice">
                  Dimensões de até 500 séries; refine um label conhecido. Esta
                  lista não representa toda a cardinalidade.
                </p>
              )}
              <div className="explorer-filters">
                {Object.entries(dimensions.data?.labels || {})
                  .sort(([a], [b]) => a.localeCompare(b))
                  .map(([k, values]) => (
                    <label key={k}>
                      {label(k)}
                      <select
                        value={filters[k] || ""}
                        onChange={(e) => {
                          const f = { ...filters };
                          if (e.target.value) f[k] = e.target.value;
                          else delete f[k];
                          update({ filters: JSON.stringify(f) }, true);
                        }}
                      >
                        <option value="">Todos — sem filtro neste label</option>
                        {filters[k] && !values.includes(filters[k]) && (
                          <option>{filters[k]}</option>
                        )}
                        {values.map((v) => (
                          <option key={v} value={v}>
                            {v || "(vazio)"}
                          </option>
                        ))}
                      </select>
                    </label>
                  ))}
              </div>
              <div className="entity-actions">
                <label>
                  Operação
                  <select
                    value={route.operation || "raw"}
                    onChange={(e) =>
                      update({ operation: e.target.value }, true)
                    }
                  >
                    <option value="raw">Valor original</option>
                    <option value="rate" disabled={metric.type !== "counter"}>
                      Taxa por segundo · contador, janela 5m
                    </option>
                    <option
                      value="increase"
                      disabled={metric.type !== "counter"}
                    >
                      Incremento · contador, janela 5m
                    </option>
                  </select>
                </label>
                <button
                  onClick={() => {
                    setSubmitted(key);
                    if (submitted === key) result.retry();
                  }}
                >
                  Consultar métrica
                </button>
                <button onClick={() => update({ filters: "" }, true)}>
                  Limpar labels
                </button>
              </div>
              {submitted !== key ? (
                <p className="notice">
                  Filtros alterados. Selecione Consultar métrica para
                  aplicá-los.
                </p>
              ) : (
                <>
                  <QueryStatus
                    query={result}
                    label="Amostras · Prometheus"
                    source={result.data?.source}
                  />
                  {result.data && (
                    <>
                      <QueryInfo
                        query={result.data.query}
                        filters={result.data.filters}
                      />
                      <Results
                        result={result.data.result}
                        title={metric.name}
                        unit={
                          displayUnit(metric.unit) +
                          (route.operation === "rate" ? "/s" : "")
                        }
                        start={result.data.start}
                        end={result.data.end}
                        step={result.data.stepSeconds}
                      />
                    </>
                  )}
                </>
              )}
            </>
          )}
        </section>
      </div>
    </>
  );
}
function VariableControl({
  variable,
  dashboard,
  values,
  props,
}: {
  variable: Variable;
  dashboard: string;
  values: Record<string, string>;
  props: Props;
}) {
  const { api, route, update } = props;
  const [snapshot, setSnapshot] = useState<{
    route: Route;
    values: Record<string, string>;
    nonce: number;
  } | null>(null);
  const loadOptions = () =>
    setSnapshot({
      route: { ...route },
      values: { ...values },
      nonce: Date.now(),
    });
  const options = useQuery<{
    values: string[];
    truncated: boolean;
    source: SourceStatus;
  }>(
    `variable:${dashboard}:${variable.name}:${snapshot?.nonce}`,
    (s) =>
      request(
        api,
        `variables/${encodeURIComponent(dashboard)}/${encodeURIComponent(variable.name)}`,
        snapshot!.route,
        { variables: JSON.stringify(snapshot!.values) },
        s,
      ),
    !!snapshot && variable.type !== "textbox",
  );
  const set = (v: string) =>
    update(
      { variables: JSON.stringify({ ...values, [variable.name]: v }) },
      true,
    );
  return (
    <div className="explorer-variable">
      <label>
        {label(variable.name)}
        {variable.type === "textbox" ? (
          <input
            aria-label={label(variable.name)}
            value={values[variable.name] || ""}
            onChange={(e) => set(e.target.value)}
            placeholder="Texto literal; vazio para todos"
          />
        ) : (
          <select
            aria-label={label(variable.name)}
            value={values[variable.name] || "__all__"}
            onFocus={() => {
              if (!snapshot) loadOptions();
            }}
            onChange={(e) => set(e.target.value)}
          >
            {variable.includeAll && (
              <option value="__all__">Todos os valores da fonte</option>
            )}
            {values[variable.name] &&
              values[variable.name] !== "__all__" &&
              !options.data?.values?.includes(values[variable.name]) && (
                <option>{values[variable.name]}</option>
              )}
            {options.data?.values?.map((v) => (
              <option key={v} value={v}>
                {v || "(vazio)"}
              </option>
            ))}
          </select>
        )}
      </label>
      {variable.type !== "textbox" && (
        <button onClick={loadOptions}>Carregar opções</button>
      )}
      {snapshot && (
        <QueryStatus
          query={options}
          label={label(variable.name)}
          source={options.data?.source}
        />
      )}{" "}
      {options.data?.truncated && (
        <small>
          Lista limitada a 2.000 valores. Não representa a cardinalidade
          completa.
        </small>
      )}
    </div>
  );
}
function Views(props: Props & { catalog: Catalog }) {
  const { api, route, refresh, update, catalog } = props;
  const [search, setSearch] = useState("");
  const dashboard = catalog.dashboards.find((d) => d.uid === route.dashboard);
  const panel = dashboard?.panels.find((p) => String(p.id) === route.panel);
  const values = {
    ...(dashboard ? defaultVariables(dashboard) : {}),
    ...readSelection(route.variables),
  };
  const [submitted, setSubmitted] = useState("");
  const key = JSON.stringify([
    route.dashboard,
    route.panel,
    route.variables,
    route.window,
    route.end,
    refresh,
  ]);
  const query = useQuery<PanelResult>(
    key,
    (s) =>
      request(
        api,
        `panels/${encodeURIComponent(route.dashboard || "")}/${route.panel}`,
        route,
        { variables: JSON.stringify(values) },
        s,
      ),
    !!panel && panel.type !== "text" && submitted === key,
  );
  useEffect(() => {
    if (panel) setSubmitted(key);
  }, [route.dashboard, route.panel, route.window, route.end, refresh]);
  return (
    <>
      <p>
        {catalog.catalogNote} As consultas rodam aqui, na fonte original, com o
        período escolhido.
      </p>
      <label>
        Encontrar visão ou painel
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </label>
      <div className="explorer-layout">
        <section className="explorer-list" aria-label="Visões disponíveis">
          {catalog.dashboards
            .filter((d) =>
              `${d.title} ${d.panels.map((p) => p.title).join(" ")}`
                .toLowerCase()
                .includes(search.toLowerCase()),
            )
            .map((d) => (
              <button
                className="metric-choice"
                aria-pressed={d.uid === dashboard?.uid}
                key={d.uid}
                onClick={() =>
                  update({
                    dashboard: d.uid,
                    panel: "",
                    variables: JSON.stringify(defaultVariables(d)),
                  })
                }
              >
                <b>{d.title}</b>
                <small>
                  {d.panels.length} painéis ·{" "}
                  {[
                    ...new Set(
                      d.panels.map((p) => p.datasource?.type).filter(Boolean),
                    ),
                  ].join(", ")}
                </small>
              </button>
            ))}
        </section>
        <section className="explorer-detail">
          {!dashboard ? (
            <div className="empty">
              Escolha uma visão, ajuste os filtros e selecione um painel. Cada
              consulta informa sua fonte e cobertura.
            </div>
          ) : (
            <>
              <h2>{dashboard.title}</h2>
              <p className="notice">
                Filtros são os da fonte. Um painel só utiliza as variáveis
                presentes na sua consulta; veja os filtros aplicados no
                resultado. Sem conversão automática de instance para host_name.
              </p>
              <details className="panel" open>
                <summary>Filtros desta visão</summary>
                <div className="explorer-filters">
                  {dashboard.templating.list.map((v) => (
                    <VariableControl
                      key={dashboard.uid + v.name}
                      variable={v}
                      dashboard={dashboard.uid}
                      values={values}
                      props={props}
                    />
                  ))}
                </div>
              </details>
              <div className="panel-selector" aria-label="Painéis desta visão">
                {dashboard.panels.map((p) => (
                  <button
                    key={p.id}
                    aria-pressed={p.id === panel?.id}
                    onClick={() => update({ panel: String(p.id) })}
                  >
                    {p.title}
                    <small>
                      {p.type} · {p.datasource?.type || "informação"}
                    </small>
                  </button>
                ))}
              </div>
              {panel && (
                <section className="selected-panel">
                  <h3>{panel.title}</h3>
                  <p>{panel.description}</p>
                  {panel.type === "text" ? (
                    <pre className="information-text">
                      {panel.options.content || "Sem conteúdo informativo"}
                    </pre>
                  ) : (
                    <>
                      <button
                        onClick={() => {
                          setSubmitted(key);
                          if (submitted === key) query.retry();
                        }}
                      >
                        Consultar painel
                      </button>
                      {submitted !== key ? (
                        <p className="notice">
                          Filtros alterados. Consulte novamente para aplicá-los.
                        </p>
                      ) : (
                        <>
                          <QueryStatus
                            query={query}
                            label="Consulta do painel"
                          />
                          {query.data?.targets.map((t) => (
                            <section key={t.refId} className="target-result">
                              <SourceBanner
                                source={t.source}
                                label={`${t.sourceName} · ${t.refId}`}
                              />
                              <QueryInfo
                                query={t.query}
                                filters={t.appliedFilters}
                              />
                              {t.result && (
                                <Results
                                  result={t.result}
                                  title={`${panel.title} · ${t.refId}`}
                                  unit={displayUnit(
                                    panel.fieldConfig?.defaults?.unit,
                                  )}
                                  start={query.data!.start}
                                  end={query.data!.end}
                                  step={query.data!.stepSeconds}
                                  legendFormat={t.legendFormat}
                                  stat={panel.type === "stat"}
                                  table={panel.type === "table"}
                                />
                              )}{" "}
                              {t.traces && (
                                <TraceTable traces={t.traces} route={route} />
                              )}
                            </section>
                          ))}
                        </>
                      )}
                    </>
                  )}
                </section>
              )}
            </>
          )}
        </section>
      </div>
    </>
  );
}
function QueryInfo({
  query,
  filters,
}: {
  query: string;
  filters: Record<string, string>;
}) {
  return (
    <details className="query-details">
      <summary>Fonte, consulta e filtros aplicados</summary>
      <p>
        {Object.keys(filters).length
          ? labelsText(filters)
          : "Sem filtros adicionais; consulta geral desta métrica ou painel."}
      </p>
      <pre>{query}</pre>
      <p>
        Até 40 séries, 360 pontos por série e 10s por consulta. Reduza período e
        escopo se atingir os limites.
      </p>
    </details>
  );
}
function Results({
  result,
  title,
  unit,
  start,
  end,
  step,
  stat,
  table,
  legendFormat,
}: {
  result: QueryResult;
  title: string;
  unit: string;
  start: string;
  end: string;
  step: number;
  stat?: boolean;
  table?: boolean;
  legendFormat?: string;
}) {
  return (
    <>
      {result.truncated && (
        <p role="status" className="notice">
          Resultado truncado. Refine os filtros; as séries ou eventos restantes
          não estão representados.
        </p>
      )}
      {result.nonfinite > 0 && (
        <p className="notice">
          {result.nonfinite} amostras indefinidas omitidas. Nenhum zero foi
          inventado.
        </p>
      )}
      {result.warnings?.map((s, i) => (
        <p className="notice" key={i}>
          {s}
        </p>
      ))}
      {result.type === "streams" ? (
        <>
          <p>
            Até 200 eventos, do mais recente ao mais antigo. Não representa
            todos os eventos da janela.
          </p>
          {!result.logs?.length ? (
            <p className="empty">Sem eventos correspondentes.</p>
          ) : (
            result.logs.map((l, i) => (
              <article className="log-entry" key={i}>
                <time>{new Date(l.timestamp).toLocaleString("pt-BR")}</time>
                <small>{labelsText(l.labels)}</small>
                <pre>{l.line}</pre>
              </article>
            ))
          )}
        </>
      ) : (
        <>
          {stat && (
            <div className="stat-series">
              {result.series.map((s, i) => {
                const last = lastFinite(s);
                return (
                  <article key={i}>
                    <strong>
                      {formatMeasurement(last?.value, unit)}
                    </strong>
                    <p>{labelsText(s.labels)}</p>
                    <small>
                      {last
                        ? `Última amostra finita: ${new Date(last.timestamp).toLocaleString("pt-BR")}`
                        : "Sem amostras finitas"}
                    </small>
                  </article>
                );
              })}
            </div>
          )}
          <MiniChart
            name={title}
            legendFormat={legendFormat}
            meta={[
              title,
              unit,
              "Séries independentes, com labels e timestamps originais. Lacunas indicam ausência de amostras.",
            ]}
            series={result.series}
            start={start}
            end={end}
            step={step}
          />
          {table && (
            <div className="chart-data">
              <table>
                <thead>
                  <tr>
                    <th>Labels da fonte</th>
                    <th>Último valor finito</th>
                    <th>Horário</th>
                  </tr>
                </thead>
                <tbody>
                  {result.series.map((s, i) => {
                    const p = lastFinite(s);
                    return (
                      <tr key={i}>
                        <td>{labelsText(s.labels)}</td>
                        <td>{p?.value ?? "Sem dados"}</td>
                        <td>{p?.timestamp || "—"}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </>
  );
}
function TraceTable({
  traces,
  route,
}: {
  traces: TraceSummary[];
  route: Route;
}) {
  return (
    <>
      <p>
        Até 50 traces na janela. O resultado limitado não representa todas as
        requisições.
      </p>
      <div className="chart-data">
        <table>
          <thead>
            <tr>
              <th>Serviço</th>
              <th>Operação</th>
              <th>Duração (ms)</th>
              <th>Trace</th>
            </tr>
          </thead>
          <tbody>
            {traces.map((t) => (
              <tr key={t.traceId}>
                <td>{t.rootServiceName}</td>
                <td>{t.rootTraceName}</td>
                <td>{t.durationMs}</td>
                <td>
                  <a href={exploreURL("tempo", route, t.traceId)}>
                    {t.traceId}
                  </a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!traces.length && (
          <p>Sem traces na janela e nos filtros escolhidos.</p>
        )}
      </div>
    </>
  );
}
function Platform({ api, route, refresh }: Props) {
  const query = useQuery<{
    inventory: {
      targets: {
        job: string;
        instance: string;
        health: string;
        lastScrape: string;
        hasError: boolean;
      }[];
      groups: {
        name: string;
        rules: {
          name: string;
          type: string;
          query: string;
          health: string;
          state: string;
          labels: Record<string, string>;
        }[];
      }[];
    } | null;
    source: SourceStatus;
    note: string;
  }>(`platform:${refresh}`, (s) => request(api, "platform", route, {}, s));
  return (
    <>
      <QueryStatus
        query={query}
        label="Alvos e regras · Prometheus"
        source={query.data?.source}
      />
      {query.data && <p>{query.data.note}</p>}
      {query.data?.inventory && (
        <>
          <h2>Alvos de coleta ({query.data.inventory.targets.length})</h2>
          <div className="chart-data">
            <table>
              <thead>
                <tr>
                  <th>Job</th>
                  <th>Instância</th>
                  <th>Coleta</th>
                  <th>Última tentativa</th>
                </tr>
              </thead>
              <tbody>
                {query.data.inventory.targets.map((t, i) => (
                  <tr key={i}>
                    <td>{t.job}</td>
                    <td>{t.instance}</td>
                    <td>
                      {t.health === "up" ? "Respondendo" : "Indisponível"}
                      {t.hasError ? " · erro de coleta" : ""}
                    </td>
                    <td>{t.lastScrape}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <h2>
            Regras configuradas (
            {query.data.inventory.groups.reduce(
              (n, g) => n + g.rules.length,
              0,
            )}
            )
          </h2>
          {query.data.inventory.groups.map((g) => (
            <section key={g.name}>
              <h3>{g.name}</h3>
              {g.rules.map((r) => (
                <details key={r.name} className="panel">
                  <summary>
                    {r.name} · {r.type} · {r.state || r.health}
                  </summary>
                  <p>{labelsText(r.labels || {})}</p>
                  <pre>{r.query}</pre>
                  <p>Estado da avaliação: {r.health}</p>
                </details>
              ))}
            </section>
          ))}
        </>
      )}
    </>
  );
}

type ProfileFrame = {
  name: string;
  depth: number;
  offset: string;
  total: string;
  self: string;
};
type ProfileResult = {
  graph: { frames: ProfileFrame[]; total: string; limited: boolean };
  source: SourceStatus;
  selector: string;
  unit: string;
  start: string;
  end: string;
  note: string;
};
export function profilePosition(frame: ProfileFrame, total: string) {
  const size = Number(total);
  return {
    left: size ? (Number(frame.offset) / size) * 100 : 0,
    width: size ? (Number(frame.total) / size) * 100 : 0,
  };
}
export function NativeProfiles({ api, route, refresh, update }: Props) {
  const catalog = useQuery<{
    types: {
      ID: string;
      name: string;
      sampleType: string;
      sampleUnit: string;
    }[];
    labels: string[];
    sources: Record<string, SourceStatus>;
  }>(`profiles:${route.window}:${route.end}:${refresh}`, (s) =>
    request(api, "profiles/catalog", route, {}, s),
  );
  const services = useQuery<{
    values: string[];
    source: SourceStatus;
    truncated: boolean;
  }>(
    `profile-services:${route.window}:${route.end}:${refresh}`,
    (s) => request(api, "profiles/labels", route, { label: "service_name" }, s),
    !!catalog.data,
  );
  const filters = readSelection(route.profileFilters);
  const [extra, setExtra] = useState("");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<ProfileFrame | null>(null);
  const labelValues = useQuery<{
    values: string[];
    source: SourceStatus;
    truncated: boolean;
  }>(
    `profile-label:${extra}:${route.window}:${route.end}`,
    (s) => request(api, "profiles/labels", route, { label: extra }, s),
    !!extra,
  );
  const key = JSON.stringify([
    route.profileType,
    route.profileFilters,
    route.window,
    route.end,
    refresh,
  ]);
  const [submitted, setSubmitted] = useState("");
  const query = useQuery<ProfileResult>(
    key,
    (s) =>
      request(
        api,
        "profiles/query",
        route,
        { type: route.profileType || "", filters: JSON.stringify(filters) },
        s,
      ),
    submitted === key && !!filters.service_name && !!route.profileType,
  );
  useEffect(() => {
    setSelected(null);
  }, [key]);
  const choose = (name: string, value: string) => {
    const next = { ...filters };
    if (value) next[name] = value;
    else delete next[name];
    update({ profileFilters: JSON.stringify(next) }, true);
  };
  return (
    <section className="panel">
      <h2>Perfis de execução · Pyroscope</h2>
      <p>
        Escolha um serviço e o tipo de amostra. O gráfico agrega as pilhas de
        funções no período; a largura representa o valor acumulado, não a
        passagem do tempo.
      </p>
      <QueryStatus
        query={catalog}
        label="Tipos de perfil"
        source={catalog.data?.sources.types}
      />
      <QueryStatus
        query={services}
        label="Serviços com perfis"
        source={services.data?.source}
      />
      <div className="explorer-filters">
        <label>
          Serviço de perfis
          <select
            value={filters.service_name || ""}
            onChange={(e) => choose("service_name", e.target.value)}
          >
            <option value="">Escolha um serviço</option>
            {services.data?.values?.map((x) => (
              <option key={x}>{x}</option>
            ))}
          </select>
        </label>
        <label>
          Tipo de perfil
          <select
            value={route.profileType || ""}
            onChange={(e) => update({ profileType: e.target.value }, true)}
          >
            <option value="">Escolha um tipo</option>
            {catalog.data?.types?.map((t) => (
              <option key={t.ID} value={t.ID}>
                {t.name} · {t.sampleType} ({t.sampleUnit})
              </option>
            ))}
          </select>
        </label>
        <label>
          Refinar por outro label
          <select value={extra} onChange={(e) => setExtra(e.target.value)}>
            <option value="">Sem label adicional</option>
            {catalog.data?.labels
              ?.filter((x) => x !== "service_name" && !x.startsWith("__"))
              .map((x) => (
                <option key={x}>{x}</option>
              ))}
          </select>
        </label>
        {extra && (
          <label>
            {extra}
            <select
              value={filters[extra] || ""}
              onChange={(e) => choose(extra, e.target.value)}
            >
              <option value="">Todos os valores</option>
              {labelValues.data?.values?.map((x) => (
                <option key={x}>{x}</option>
              ))}
            </select>
          </label>
        )}
      </div>
      {extra && (
        <QueryStatus
          query={labelValues}
          label="Valores do label"
          source={labelValues.data?.source}
        />
      )}
      {(services.data?.truncated || labelValues.data?.truncated) && (
        <p className="notice">Lista de valores limitada a 2.000 entradas.</p>
      )}
      <p>
        Filtros aplicados: {labelsText(filters)}. A seleção de
        servidor/container de outras páginas não é convertida automaticamente.
      </p>
      <div className="entity-actions">
        <button
          disabled={!filters.service_name || !route.profileType}
          onClick={() => {
            setSubmitted(key);
            if (submitted === key) query.retry();
          }}
        >
          Consultar perfil
        </button>
        <button
          onClick={() =>
            update(
              {
                profileFilters: JSON.stringify(
                  filters.service_name
                    ? { service_name: filters.service_name }
                    : {},
                ),
              },
              true,
            )
          }
        >
          Limpar labels adicionais
        </button>
        <a href={exploreURL("pyroscope", route)}>Abrir ferramenta integrada</a>
      </div>
      {submitted !== key ? (
        <p className="notice">
          Selecione Consultar perfil para aplicar os filtros e o período atuais.
        </p>
      ) : (
        <>
          <QueryStatus
            query={query}
            label="Perfil agregado"
            source={query.data?.source}
          />
          {query.data && (
            <>
              <p>{query.data.note}</p>
              <p>
                {new Date(query.data.start).toLocaleString("pt-BR")} —{" "}
                {new Date(query.data.end).toLocaleString("pt-BR")} · total{" "}
                {query.data.graph.total} {query.data.unit}
              </p>
              <p className="notice">
                Árvore limitada aos nós mais relevantes pela fonte
                (maxNodes=200); funções menores podem aparecer agrupadas.
                Valores são preservados sem conversão de unidade.
              </p>
              <code>{query.data.selector}</code>
              <label>
                Localizar função
                <input
                  type="search"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </label>
              <div
                className="flamegraph"
                aria-label="Árvore de funções do perfil"
                style={{
                  height:
                    (Math.max(
                      0,
                      ...query.data.graph.frames.map((f) => f.depth),
                    ) +
                      1) *
                    25,
                }}
              >
                {query.data.graph.frames.map((f, i) => {
                  const pos = profilePosition(f, query.data!.graph.total);
                  return (
                    <button
                      key={i}
                      title={`${f.name} · total ${f.total} · próprio ${f.self}`}
                      aria-label={`${f.name}, nível ${f.depth}, total ${f.total}, próprio ${f.self}`}
                      onClick={() => setSelected(f)}
                      className={
                        search &&
                        !f.name.toLowerCase().includes(search.toLowerCase())
                          ? "dimmed"
                          : ""
                      }
                      style={{
                        top: f.depth * 25,
                        left: `${pos.left}%`,
                        width: `${pos.width}%`,
                      }}
                    >
                      {f.name}
                    </button>
                  );
                })}
              </div>
              {selected && (
                <p role="status">
                  <b>{selected.name}</b> · total com chamadas: {selected.total}{" "}
                  · próprio: {selected.self} {query.data.unit} · profundidade:{" "}
                  {selected.depth}
                </p>
              )}
              <details>
                <summary>Tabela acessível de funções e valores exatos</summary>
                <div className="chart-data">
                  <table>
                    <thead>
                      <tr>
                        <th>Função</th>
                        <th>Nível</th>
                        <th>Incluindo chamadas</th>
                        <th>Próprio</th>
                      </tr>
                    </thead>
                    <tbody>
                      {query.data.graph.frames
                        .filter((f) =>
                          f.name.toLowerCase().includes(search.toLowerCase()),
                        )
                        .map((f, i) => (
                          <tr key={i}>
                            <td>{f.name}</td>
                            <td>{f.depth}</td>
                            <td>{f.total}</td>
                            <td>{f.self}</td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </div>
              </details>
            </>
          )}
        </>
      )}
    </section>
  );
}
