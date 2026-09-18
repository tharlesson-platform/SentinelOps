import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  AppWindow,
  Beaker,
  BookOpen,
  Boxes,
  ChevronRight,
  CircleCheck,
  Container,
  FileArchive,
  FileText,
  Gauge,
  LogOut,
  Moon,
  RadioTower,
  Rocket,
  Search,
  Server,
  ShieldCheck,
  Sun,
  TriangleAlert,
  Waves,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { UserManager } from "oidc-client-ts";
import {
  Agent,
  API,
  Asset,
  DataLifecycleDomain,
  DataLifecycleRequest,
  ObservabilityOverview,
  Scenario,
  Service,
  SyntheticRun,
} from "./api";
import {
  ObservabilityPage,
  ObservabilityView,
  SourceBanner,
} from "./Observability";
import { Page, useNavigation } from "./navigation";
import tqiLogo from "./assets/tqi-logo.svg";

const glossary = [
  ["Availability", "Percentual de tentativas concluídas com sucesso."],
  ["Latency p95", "95% das respostas terminam abaixo desse valor."],
  ["Error rate", "Proporção de operações que falham."],
  ["Throughput", "Quantidade de trabalho concluído por unidade de tempo."],
  ["SLO", "Meta explícita de confiabilidade em uma janela."],
  ["Error budget", "Falha tolerável antes de colocar o SLO em risco."],
  ["Burn rate", "Velocidade de consumo do error budget."],
  ["Baseline", "Período ou versão usada como referência."],
];

export default function App() {
  const { t } = useTranslation();
  const { route, update, back } = useNavigation();
  const page = route.page;
  const setPage = (page: Page) => update({ page });
  const [token, setToken] = useState(
    () => sessionStorage.getItem("sentinel-token") || "",
  );
  const api = useMemo(() => new API(token), [token]);
  const oidc = useMemo(() => oidcManager(), []);
  const [theme, setTheme] = useState(
    () => localStorage.getItem("sentinel-theme") || "dark",
  );
  const [services, setServices] = useState<Service[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [scenarios, setScenarios] = useState<Scenario[]>([]);
  const [runs, setRuns] = useState<SyntheticRun[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [refreshedAt, setRefreshedAt] = useState<Date | null>(null);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [catalogQuery, setCatalogQuery] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const oidcCallbackStarted = useRef(false);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("sentinel-theme", theme);
  }, [theme]);
  useEffect(() => {
    if (
      token ||
      !oidc ||
      oidcCallbackStarted.current ||
      !new URLSearchParams(location.search).has("code")
    )
      return;
    oidcCallbackStarted.current = true;
    oidc
      .signinRedirectCallback()
      .then((user) => {
        const returnTo = (user.state as { returnTo?: string } | undefined)
          ?.returnTo;
        history.replaceState(
          {},
          document.title,
          returnTo?.startsWith(import.meta.env.BASE_URL) &&
            !returnTo.startsWith("//")
            ? returnTo
            : location.pathname,
        );
        window.dispatchEvent(new PopStateEvent("popstate"));
        sessionStorage.setItem("sentinel-token", user.access_token);
        setToken(user.access_token);
      })
      .catch((e) => setError(String(e)));
  }, [oidc, token]);
  useEffect(() => {
    if (!token) return;
    let active = true;
    setLoading(true);
    Promise.all([
      api.services(),
      api.agents(),
      api.scenarios(),
      api.syntheticRuns(),
    ])
      .then(([s, a, sc, runs]) => {
        if (!active) return;
        setServices(s);
        setAgents(a);
        setScenarios(sc);
        setRuns(runs);
        setRefreshedAt(new Date());
        setError("");
      })
      .catch((e) => {
        if (active) setError(String(e));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, token, refreshNonce]);
  if (!token)
    return (
      <Login
        callbackError={error}
        api={api}
        oidc={oidc}
        onLogin={(value) => {
          sessionStorage.setItem("sentinel-token", value);
          setToken(value);
        }}
      />
    );
  const nav: [Page, string, typeof Gauge][] = [
    ["overview", t("overview"), Gauge],
    ["hosts", "Servidores", Server],
    ["docker", "Containers", Container],
    ["logs", "Registros de eventos", FileText],
    ["metrics", "Métricas", Activity],
    ["apm", "APM", AppWindow],
    ["traces", "Requisições", Waves],
    ["profiles", "Perfis de execução", Gauge],
    ["catalog", t("catalog"), Boxes],
    ["tests", t("tests"), Beaker],
    ["releases", t("releases"), Rocket],
    ["agents", t("agents"), RadioTower],
    ["governance", "Governança de dados", FileArchive],
    ["help", t("help"), BookOpen],
  ];
  const logout = () => {
    sessionStorage.removeItem("sentinel-token");
    setToken("");
    if (oidc) void oidc.signoutRedirect();
  };
  const controlPlane = error
    ? "API de operação indisponível"
    : loading
      ? "Consultando API de operação"
      : refreshedAt
        ? `API de operação consultada às ${refreshedAt.toLocaleTimeString("pt-BR")}`
        : "API de operação sem dados";
  return (
    <div className="shell">
      <a className="skip-link" href="#conteudo">
        Pular para o conteúdo
      </a>
      <aside className="sidebar">
        <div className="brand">
          <img className="tqi-logo" src={tqiLogo} alt="TQI" />
          <div>
            <strong>SentinelOps</strong>
            <small>Observabilidade</small>
          </div>
        </div>
        <button
          className="mobile-menu-toggle"
          aria-expanded={menuOpen}
          aria-controls="main-menu"
          onClick={() => setMenuOpen(!menuOpen)}
        >
          Menu de navegação
        </button>
        <nav
          id="main-menu"
          className={menuOpen ? "menu-open" : "menu-closed"}
          aria-label="Principal"
        >
          {nav.map(([id, label, Icon]) => (
            <button
              key={id}
              className={page === id ? "active" : ""}
              aria-current={page === id ? "page" : undefined}
              onClick={() => {
                setMenuOpen(false);
                update({
                  page: id,
                  ...(id === "hosts" || id === "docker"
                    ? { query: "", hostFilter: "", environment: "" }
                    : {}),
                });
              }}
            >
              <Icon size={19} />
              <span>{label}</span>
            </button>
          ))}
        </nav>
        <div className={`sidebar-foot ${error ? "unavailable" : ""}`}>
          <div className="control-plane">
            <span className="pulse" />
            {controlPlane}
          </div>
          <button onClick={logout}>
            <LogOut size={18} />
            {t("logout")}
          </button>
        </div>
      </aside>
      <main>
        <header className="topbar">
          <form
            className="search"
            onSubmit={(e) => {
              e.preventDefault();
              setPage("catalog");
            }}
          >
            <Search size={18} />
            <input
              aria-label="Buscar no catálogo"
              value={catalogQuery}
              onChange={(e) => setCatalogQuery(e.target.value)}
              placeholder="Buscar no catálogo"
            />
            <button aria-label="Pesquisar no catálogo">Buscar</button>
          </form>
          <div className="tools">
            <button
              aria-label="Atualizar dados"
              onClick={() => {
                update({ end: Math.floor(Date.now() / 1000) }, true);
                setRefreshNonce((value) => value + 1);
              }}
            >
              Atualizar
            </button>
            <button
              aria-label="Alternar tema"
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            >
              {theme === "dark" ? <Sun size={19} /> : <Moon size={19} />}
            </button>
          </div>
        </header>
        <section className="content" id="conteudo">
          <nav className="breadcrumbs" aria-label="Caminho de navegação">
            <button onClick={back}>← Voltar</button>
            <button
              onClick={() => {
                setMenuOpen(false);
                update({
                  page: "overview",
                  context: {},
                  query: "",
                  hostFilter: "",
                  environment: "",
                });
              }}
            >
              Início
            </button>
            <span aria-hidden="true">/</span>
            {page === "metrics" && (
              <button
                onClick={() =>
                  setPage(route.context.container ? "docker" : "hosts")
                }
              >
                {route.context.container ? "Containers" : "Servidores"}
              </button>
            )}
            <span aria-current="page">
              {nav.find(([id]) => id === page)?.[1]}
            </span>
            <a href="/">Portal de ferramentas ↗</a>
          </nav>
          {error && (
            <div className="error">
              <TriangleAlert size={18} />
              {error}
            </div>
          )}
          {loading && <div className="loading">Atualizando sinais…</div>}
          {page === "overview" && (
            <Overview
              api={api}
              services={services}
              agents={agents}
              scenarios={scenarios}
              runs={runs}
              refreshedAt={refreshedAt}
              t={t}
              refresh={refreshNonce}
              onPage={setPage}
            />
          )}{" "}
          {(
            [
              "hosts",
              "docker",
              "logs",
              "metrics",
              "apm",
              "traces",
              "profiles",
            ] as Page[]
          ).includes(page) && (
            <ObservabilityView
              page={page as ObservabilityPage}
              api={api}
              route={route}
              update={update}
              refresh={refreshNonce}
            />
          )}{" "}
          {page === "catalog" && (
            <Catalog
              api={api}
              services={services}
              query={catalogQuery}
              onQuery={setCatalogQuery}
            />
          )}{" "}
          {page === "tests" && (
            <TestStudio
              services={services}
              scenarios={scenarios}
              api={api}
              onSaved={(s) =>
                setScenarios((x) => [...x.filter((i) => i.name !== s.name), s])
              }
            />
          )}{" "}
          {page === "releases" && <ReleaseView />}{" "}
          {page === "agents" && <Agents agents={agents} />}{" "}
          {page === "governance" && <DataGovernance api={api} />}{" "}
          {page === "help" && <Help />}
        </section>
      </main>
    </div>
  );
}

function Login({
  api,
  oidc,
  onLogin,
  callbackError,
}: {
  callbackError?: string;
  api: API;
  oidc: UserManager | null;
  onLogin: (token: string) => void;
}) {
  const { t } = useTranslation();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    const f = new FormData(e.currentTarget);
    try {
      const out = await api.login(
        String(f.get("username")),
        String(f.get("password")),
      );
      onLogin(out.accessToken);
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-page">
      <div className="ambient one" />
      <div className="ambient two" />
      <form className="login-card" onSubmit={submit}>
        <img className="tqi-logo" src={tqiLogo} alt="TQI" />
        <p className="eyebrow">TQI · OBSERVABILIDADE</p>
        <h1>{t("loginTitle")}</h1>
        <p>
          {oidc
            ? "Use sua conta corporativa para continuar."
            : "Entre com o usuário e a senha fornecidos pela equipe responsável."}
        </p>
        {(error || callbackError) && (
          <div className="error" role="alert">
            {error || callbackError}
          </div>
        )}
        {oidc ? (
          <button
            type="button"
            className="primary"
            onClick={() => {
              setBusy(true);
              void oidc
                .signinRedirect({
                  state: { returnTo: location.pathname + location.search },
                })
                .catch(() => {
                  setError(
                    "Não foi possível iniciar a autenticação. Tente novamente.",
                  );
                  setBusy(false);
                });
            }}
            disabled={busy}
          >
            Entrar com conta corporativa
            <ChevronRight size={18} />
          </button>
        ) : (
          <>
            <label>
              {t("username")}
              <input name="username" autoComplete="username" required />
            </label>
            <label>
              {t("password")}
              <input
                name="password"
                type="password"
                autoComplete="current-password"
                required
              />
            </label>
            <button className="primary" disabled={busy}>
              {busy ? "Autenticando…" : t("login")}
              <ChevronRight size={18} />
            </button>
          </>
        )}
        <small>
          Precisa de acesso? Procure a equipe responsável pela plataforma.
        </small>
      </form>
    </div>
  );
}

function oidcManager(): UserManager | null {
  const cfg = window.__SENTINEL_CONFIG__;
  if (cfg?.authMode !== "oidc" || !cfg.oidcAuthority || !cfg.oidcClientId)
    return null;
  return new UserManager({
    authority: cfg.oidcAuthority,
    client_id: cfg.oidcClientId,
    redirect_uri: location.origin + import.meta.env.BASE_URL,
    post_logout_redirect_uri: location.origin + import.meta.env.BASE_URL,
    response_type: "code",
    scope: cfg.oidcScope || "openid profile sentinelops.api",
    automaticSilentRenew: true,
    monitorSession: true,
  });
}

function Overview({
  api,
  services,
  agents,
  scenarios,
  runs,
  refreshedAt,
  t,
  refresh,
  onPage,
}: {
  api: API;
  services: Service[];
  agents: Agent[];
  scenarios: Scenario[];
  runs: SyntheticRun[];
  refreshedAt: Date | null;
  t: (k: string) => string;
  refresh: number;
  onPage: (page: Page) => void;
}) {
  const offline = agents.filter((a) => a.status === "offline").length;
  const source = refreshedAt
    ? `Catálogo/API em ${refreshedAt.toLocaleString("pt-BR")}`
    : "Aguardando consulta à API";
  const latest = runs[0];
  const runStatus = latest
    ? `${latest.status} · ${latest.scenario}`
    : "Sem dados";
  const [observability, setObservability] = useState<ObservabilityOverview>();
  const [telemetryError, setTelemetryError] = useState("");
  const [telemetryLoading, setTelemetryLoading] = useState(true);
  const [telemetryRetry, setTelemetryRetry] = useState(0);
  useEffect(() => {
    let active = true;
    setObservability(undefined);
    setTelemetryError("");
    setTelemetryLoading(true);
    api
      .observabilityOverview()
      .then((value) => active && setObservability(value))
      .catch((e) => {
        if (active) setTelemetryError(String(e));
      })
      .finally(() => {
        if (active) setTelemetryLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, refresh, telemetryRetry]);
  const signal = (name: string) =>
    observability?.values[name] == null
      ? "Sem dados"
      : String(Math.round(observability.values[name]!));
  const telemetrySource =
    observability?.sources.prometheus.state === "available"
      ? "Prometheus consultado"
      : observability?.sources.prometheus.message ||
        (telemetryLoading
          ? "Consultando telemetria…"
          : telemetryError
            ? "Falha ao consultar telemetria"
            : "Sem dados");
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">TQI · VISÃO OPERACIONAL</p>
          <h1>{t("welcome")}</h1>
          <p>{t("subtitle")}</p>
        </div>
        <span className="time-range">{source}</span>
      </div>
      {telemetryLoading && (
        <p role="status">Consultando resumo de telemetria…</p>
      )}
      {telemetryError && (
        <div role="alert" className="error">
          {telemetryError}
          <button onClick={() => setTelemetryRetry((n) => n + 1)}>
            Tentar novamente
          </button>
        </div>
      )}
      <div className="metrics">
        <Metric
          icon={CircleCheck}
          label="Servidores com coleta ativa"
          value={signal("hostsUp")}
          detail={telemetrySource}
        />
        <Metric
          icon={Server}
          label={t("services")}
          value={signal("services")}
          detail={telemetrySource}
        />
        <Metric
          icon={Container}
          label="Containers"
          value={signal("containers")}
          detail={telemetrySource}
        />
        <Metric
          icon={RadioTower}
          label={t("offlineAgents")}
          value={refreshedAt ? String(offline) : "Sem dados"}
          tone={offline ? "bad" : ""}
          detail={source}
        />
        <Metric
          icon={Beaker}
          label={t("activeTests")}
          value={runStatus}
          detail={
            latest?.finishedAt
              ? `evidência: ${new Date(latest.finishedAt).toLocaleString("pt-BR")}`
              : `${scenarios.filter((s) => s.enabled).length} cenários configurados; sem execução`
          }
        />
      </div>
      <SourceBanner
        source={observability?.sources.prometheus}
        label="Resumo · Prometheus"
      />
      <div className="grid two">
        <section className="panel">
          <h2>O que você precisa investigar?</h2>
          <p>
            Escolha o recurso para abrir os gráficos e os eventos
            correspondentes.
          </p>
          <div className="overview-actions">
            <button onClick={() => onPage("hosts")}>
              <Server />
              Servidores<span>Processamento, memória, disco e rede</span>
            </button>
            <button onClick={() => onPage("docker")}>
              <Container />
              Containers<span>Busca por nome e servidor</span>
            </button>
            <button onClick={() => onPage("logs")}>
              <FileText />
              Registros de eventos<span>Mensagens e falhas no período</span>
            </button>
            <button onClick={() => onPage("apm")}>
              <Activity />
              Aplicações<span>Requisições, erros e tempo de resposta</span>
            </button>
          </div>
        </section>
        <section className="panel">
          <h2>Coleta e evidências</h2>
          <p>
            {services.length} serviços no catálogo · {agents.length} agentes
            registrados.
          </p>
          <p>
            {refreshedAt
              ? `${offline} agentes reportados offline na última consulta.`
              : "Aguardando a consulta do inventário."}
          </p>
          <p>
            Último teste: {runStatus}.{" "}
            {latest?.finishedAt
              ? new Date(latest.finishedAt).toLocaleString("pt-BR")
              : "Nenhuma execução concluída informada."}
          </p>
          <p>
            Estado de coleta e resultado de testes não representam a saúde de
            toda a plataforma.
          </p>
          <button onClick={() => onPage("agents")}>
            Ver agentes e última comunicação
          </button>
          <button onClick={() => onPage("tests")}>
            Ver testes configurados
          </button>
        </section>
      </div>
    </>
  );
}
function Metric({
  icon: Icon,
  label,
  value,
  detail,
  tone = "",
}: {
  icon: typeof Gauge;
  label: string;
  value: string;
  detail?: string;
  tone?: string;
}) {
  return (
    <article className={`metric ${tone}`}>
      <div className="metric-icon">
        <Icon size={20} />
      </div>
      <p>{label}</p>
      <strong>{value}</strong>
      {detail && <small>{detail}</small>}
    </article>
  );
}
function PanelTitle({
  icon: Icon,
  title,
  subtitle,
}: {
  icon: typeof Gauge;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="panel-title">
      <div>
        <Icon size={19} />
        <h2>{title}</h2>
      </div>
      <p>{subtitle}</p>
    </div>
  );
}

function Catalog({
  api,
  services,
  query,
  onQuery,
}: {
  api: API;
  services: Service[];
  query: string;
  onQuery: (value: string) => void;
}) {
  const [assets, setAssets] = useState<Asset[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const normalized = query.toLowerCase();
  const shown = services.filter((s) =>
    (s.displayName + s.name + s.ownerTeam).toLowerCase().includes(normalized),
  );
  useEffect(() => {
    let active = true;
    const timer = window.setTimeout(() => {
      setLoading(true);
      api
        .searchAssets(query)
        .then((result) => {
          if (active) {
            setAssets(result.items);
            setNextCursor(result.nextCursor || "");
            setError("");
          }
        })
        .catch((e) => active && setError(String(e)))
        .finally(() => active && setLoading(false));
    }, 250);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [api, query]);
  async function loadMore() {
    if (!nextCursor) return;
    setLoading(true);
    try {
      const result = await api.searchAssets(query, nextCursor);
      setAssets((current) => [...current, ...result.items]);
      setNextCursor(result.nextCursor || "");
      setError("");
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">SERVIÇOS E ATIVOS</p>
          <h1>Serviços e ativos sob responsabilidade</h1>
          <p>
            Inventário por ID estável, origem e lifecycle. Saúde só aparece
            quando houver evidência própria.
          </p>
        </div>
      </div>
      <div className="filter">
        <Search size={18} />
        <input
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          aria-label="Filtrar catálogo"
          placeholder="Filtrar por nome, ID, tipo, site ou responsável"
        />
      </div>
      <div className="service-list">
        {shown.map((s) => (
          <article key={s.id} className="service-card">
            <div className="service-avatar">
              <AppWindow />
            </div>
            <div>
              <h2>{s.displayName}</h2>
              <code>{s.name}</code>
              <p>{s.description || "Sem descrição operacional."}</p>
            </div>
            <div className="service-meta">
              <span>Tier {s.tier}</span>
              <span>{s.ownerTeam}</span>
              <b>Estado de telemetria: não consultado</b>
            </div>
            <ChevronRight />
          </article>
        ))}
        {!shown.length && (
          <Empty text="Nenhum serviço encontrado. Use sentinelctl service apply." />
        )}
      </div>
      <section className="panel">
        <PanelTitle
          icon={Server}
          title="Ativos inventariados"
          subtitle="Consulta limitada e paginada; lifecycle não representa saúde operacional."
        />
        {error && <div className="error">{error}</div>}
        <div className="agent-table">
          <div className="table-head">
            <span>Ativo</span>
            <span>Tipo / site</span>
            <span>Owner</span>
            <span>Origem</span>
            <span>Lifecycle</span>
          </div>
          {assets.map((a) => (
            <div className="table-row" key={a.id}>
              <div>
                <b>{a.name}</b>
                <br />
                <code>{a.assetId}</code>
              </div>
              <span>{a.kind + (a.site ? " · " + a.site : "")}</span>
              <span>{a.ownerTeam || "Não definido"}</span>
              <span>{a.source}</span>
              <strong className={a.lifecycle}>
                <i />
                {a.lifecycle}
              </strong>
            </div>
          ))}
          {!loading && !assets.length && (
            <Empty text="Nenhum ativo encontrado para esta consulta." />
          )}
        </div>
        {loading && <div className="loading">Consultando catálogo…</div>}
        {nextCursor && (
          <button
            className="primary compact"
            onClick={() => void loadMore()}
            disabled={loading}
          >
            Carregar mais ativos
          </button>
        )}
      </section>
    </>
  );
}

function TestStudio({
  services,
  scenarios,
  api,
  onSaved,
}: {
  services: Service[];
  scenarios: Scenario[];
  api: API;
  onSaved: (s: Scenario) => void;
}) {
  const [open, setOpen] = useState(false);
  const [message, setMessage] = useState("");
  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    try {
      const s = await api.saveScenario({
        name: String(f.get("name")),
        serviceRef: String(f.get("service")),
        environment: String(f.get("environment")),
        type: "http",
        spec: {
          url: String(f.get("url")),
          method: "GET",
          timeout: String(f.get("timeout")),
          assertions: [
            { type: "status_exact", expected: Number(f.get("expectedStatus")) },
            { type: "latency_max_ms", maxMs: Number(f.get("maxMs")) },
          ],
          headers: { "X-Synthetic-Test": "sentinelops" },
        },
      });
      onSaved(s);
      setOpen(false);
      setMessage(
        "Cenário HTTP publicado. A execução permanece inconclusiva até que o runner alcance um alvo autorizado.",
      );
    } catch (err) {
      setMessage(String(err));
    }
  }
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">TESTES SINTÉTICOS</p>
          <h1>Testes que explicam a falha</h1>
          <p>Crie sinais sintéticos seguros sem começar por código.</p>
        </div>
        <button className="primary compact" onClick={() => setOpen(true)}>
          <Beaker size={18} />
          Novo cenário
        </button>
      </div>
      {message && <div className="notice">{message}</div>}
      <div className="scenario-grid">
        {scenarios.map((s) => (
          <article className="scenario-card" key={s.id}>
            <div>
              <span className="type">{s.type}</span>
              <span className="version">v{s.version}</span>
            </div>
            <h2>{s.name}</h2>
            <p>
              {s.serviceRef} · {s.environment}
            </p>
            <div className="scenario-status">
              <span className="pulse" />
              Configuração ativa; consulte a evidência da execução antes de
              aprovar
            </div>
          </article>
        ))}
        {!scenarios.length && (
          <Empty text="Publique o primeiro cenário para iniciar o monitoramento sintético." />
        )}
      </div>
      {open && (
        <dialog
          className="scenario-dialog"
          aria-labelledby="scenario-title"
          ref={(node) => {
            if (node && !node.open) node.showModal();
          }}
          onCancel={() => setOpen(false)}
        >
          <form
            className="modal"
            onSubmit={save}
            onMouseDown={(e) => e.stopPropagation()}
          >
            <p className="eyebrow">NOVO TESTE</p>
            <h2 id="scenario-title">Novo teste HTTP</h2>
            <div className="form-grid">
              <Field
                label="Nome"
                help="Identificador DNS-safe. Ex.: api-health"
              >
                <input name="name" required pattern="[a-z0-9][a-z0-9-]{1,62}" />
              </Field>
              <Field label="Serviço" help="Associa evidências e ownership.">
                <select name="service" required>
                  {services.map((s) => (
                    <option key={s.id} value={s.name}>
                      {s.displayName}
                    </option>
                  ))}
                </select>
              </Field>
              <Field
                label="Ambiente"
                help="Evita misturar baseline de DEV e PRD."
              >
                <select name="environment">
                  <option>development</option>
                  <option>staging</option>
                  <option>production</option>
                </select>
              </Field>
              <Field
                label="Tipo"
                help="Somente HTTP possui runner distribuído habilitado."
              >
                <input value="HTTP" readOnly />
              </Field>
              <Field
                label="Endpoint"
                help="Hostname e IP efetivo devem constar na allowlist do runner."
              >
                <input
                  name="url"
                  type="url"
                  required
                  placeholder="https://app.example.com/health"
                />
              </Field>
              <Field
                label="Status esperado"
                help="Código exato; HTTP 200 não é sucesso sozinho."
              >
                <input
                  name="expectedStatus"
                  type="number"
                  min="100"
                  max="599"
                  defaultValue="200"
                  required
                />
              </Field>
              <Field
                label="Latência máxima (ms)"
                help="Orçamento observado em cada execução."
              >
                <input
                  name="maxMs"
                  type="number"
                  min="1"
                  max="120000"
                  defaultValue="10000"
                  required
                />
              </Field>
              <Field label="Timeout" help="Limite total; recomendado: 10s.">
                <input name="timeout" defaultValue="10s" required />
              </Field>
            </div>
            <div className="modal-actions">
              <button type="button" onClick={() => setOpen(false)}>
                Cancelar
              </button>
              <button className="primary">
                Publicar cenário
                <ChevronRight size={18} />
              </button>
            </div>
          </form>
        </dialog>
      )}
    </>
  );
}
function Field({
  label,
  help,
  children,
}: {
  label: string;
  help: string;
  children: React.ReactNode;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
      <small>{help}</small>
    </label>
  );
}
function ReleaseView() {
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">VALIDAÇÃO DE ENTREGAS</p>
          <h1>Validação pós-deploy</h1>
          <p>Checks versionados; ausência de evidência nunca vira sucesso.</p>
        </div>
      </div>
      <section className="panel">
        <PanelTitle
          icon={Rocket}
          title="Nenhuma release selecionada"
          subtitle="A tela ainda não consulta validações vinculadas a commit, digest e janela."
        />
        <Empty text="Sem evidência de release. Resultado do gate permanece INCONCLUSIVE até a consulta de dados reais." />
      </section>
    </>
  );
}
function Agents({ agents }: { agents: Agent[] }) {
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">AGENTES DE COLETA</p>
          <h1>Execução onde o sistema vive</h1>
          <p>Localidade, capabilities e heartbeat sem abrir a rede privada.</p>
        </div>
      </div>
      <div className="agent-table">
        <div className="table-head">
          <span>Agente</span>
          <span>Localidade</span>
          <span>Ambiente</span>
          <span>Heartbeat</span>
          <span>Status</span>
        </div>
        {agents.map((a) => (
          <div className="table-row" key={a.id}>
            <b>{a.name}</b>
            <span>{a.location || a.region}</span>
            <span>{a.environment}</span>
            <span>
              {a.lastHeartbeat
                ? new Date(a.lastHeartbeat).toLocaleString()
                : "Nunca"}
            </span>
            <strong className={a.status}>
              <i />
              {a.status}
            </strong>
          </div>
        ))}
        {!agents.length && <Empty text="Nenhum agente registrado." />}
      </div>
    </>
  );
}
function DataGovernance({ api }: { api: API }) {
  const [items, setItems] = useState<DataLifecycleRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [domains, setDomains] = useState<DataLifecycleDomain[]>([
    "control-plane-metadata",
  ]);
  const domainOptions: DataLifecycleDomain[] = [
    "control-plane-metadata",
    "operational-evidence",
    "telemetry-references",
  ];
  async function refresh() {
    setLoading(true);
    try {
      setItems(await api.dataLifecycleRequests());
      setError("");
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void refresh();
  }, [api]);
  function toggleDomain(domain: DataLifecycleDomain) {
    setDomains((current) =>
      current.includes(domain)
        ? current.filter((item) => item !== domain)
        : [...current, domain],
    );
  }
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    if (!domains.length) {
      setError("Selecione pelo menos um domínio.");
      return;
    }
    try {
      const created = await api.createDataLifecycleRequest({
        requestType: String(form.get("requestType")) as "export" | "erasure",
        reason: String(form.get("reason")).trim(),
        scope: { subjectRef: String(form.get("subjectRef")).trim(), domains },
      });
      setItems((current) => [created, ...current]);
      setMessage(
        "Solicitação registrada e auditada. Uma segunda pessoa com Platform Administrator deve aprová-la.",
      );
      setError("");
      e.currentTarget.reset();
      setDomains(["control-plane-metadata"]);
    } catch (err) {
      setError(String(err));
    }
  }
  async function approve(id: string) {
    try {
      const updated = await api.approveDataLifecycleRequest(id);
      setItems((current) =>
        current.map((item) => (item.id === id ? updated : item)),
      );
      setMessage(
        "Solicitação aprovada. Nenhuma exportação ou exclusão foi iniciada.",
      );
      setError("");
    } catch (err) {
      setError(String(err));
    }
  }
  async function execute(item: DataLifecycleRequest) {
    if (
      !window.confirm(
        `Confirmar exclusão do perfil e vínculos RBAC de ${item.scope.subjectRef}? Esta ação não pode ser desfeita.`,
      )
    )
      return;
    try {
      const updated = await api.executeDataLifecycleRequest(item.id);
      setItems((current) =>
        current.map((entry) => (entry.id === item.id ? updated : entry)),
      );
      setMessage(
        "Erasure de identidade do control plane concluído. Outros domínios não foram alterados.",
      );
      setError("");
    } catch (err) {
      setError(String(err));
    }
  }
  function executable(item: DataLifecycleRequest) {
    return (
      item.status === "approved" &&
      item.requestType === "erasure" &&
      item.scope.domains.length === 1 &&
      item.scope.domains[0] === "control-plane-metadata"
    );
  }
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">DATA GOVERNANCE</p>
          <h1>Solicitações de ciclo de vida</h1>
          <p>
            Fluxo de quatro olhos para exportação ou exclusão. Apenas erasure de
            identidade do control plane possui executor.
          </p>
        </div>
      </div>
      <div className="notice">
        Use uma referência pseudônima, não e-mail, nome ou conteúdo do titular.
        Esta tela é exclusiva de Platform Administrator; a API reaplica a
        autorização.
      </div>
      {error && <div className="error">{error}</div>}
      {message && <div className="notice">{message}</div>}
      <section className="panel">
        <PanelTitle
          icon={FileArchive}
          title="Nova solicitação"
          subtitle="Escopo limitado e auditado; exportação e outros backends ainda não têm executor."
        />
        <form className="form-grid" onSubmit={submit}>
          <Field
            label="Operação"
            help="Exportar ou apagar após política e executor homologados."
          >
            <select name="requestType" defaultValue="export">
              <option value="export">Exportação</option>
              <option value="erasure">Exclusão</option>
            </select>
          </Field>
          <Field
            label="Referência pseudônima"
            help="ID estável sem e-mail, espaços ou dados pessoais brutos."
          >
            <input
              name="subjectRef"
              required
              pattern="[A-Za-z0-9][A-Za-z0-9._:-]{0,127}"
              maxLength={128}
              placeholder="employee-opaque-123"
            />
          </Field>
          <Field
            label="Motivo"
            help="Registro operacional conciso, até 1.024 caracteres."
          >
            <textarea name="reason" required maxLength={1024} rows={3} />
          </Field>
          <fieldset className="field">
            <span>Domínios</span>
            <div className="checkboxes">
              {domainOptions.map((domain) => (
                <label key={domain}>
                  <input
                    type="checkbox"
                    checked={domains.includes(domain)}
                    onChange={() => toggleDomain(domain)}
                  />
                  {domain}
                </label>
              ))}
            </div>
            <small>Selecione apenas fontes aprovadas no pedido.</small>
          </fieldset>
          <div className="modal-actions">
            <button className="primary">
              Registrar solicitação
              <ChevronRight size={18} />
            </button>
          </div>
        </form>
      </section>
      <section className="panel">
        <PanelTitle
          icon={ShieldCheck}
          title="Fila auditável"
          subtitle="O solicitante não pode aprovar o próprio pedido; execução exige confirmação adicional."
        />
        {loading ? (
          <div className="loading">Consultando solicitações…</div>
        ) : (
          <div className="agent-table">
            <div className="table-head">
              <span>Tipo / referência</span>
              <span>Escopo</span>
              <span>Solicitante</span>
              <span>Estado</span>
              <span>Ação</span>
            </div>
            {items.map((item) => (
              <div className="table-row" key={item.id}>
                <div>
                  <b>
                    {item.requestType === "export" ? "Exportação" : "Exclusão"}
                  </b>
                  <br />
                  <code>{item.scope.subjectRef}</code>
                </div>
                <span>{item.scope.domains.join(", ")}</span>
                <span>
                  {item.requestedBy}
                  {item.approvedBy && <> → {item.approvedBy}</>}
                </span>
                <strong className={item.status}>
                  <i />
                  {item.status}
                </strong>
                <span>
                  {item.status === "requested" ? (
                    <button
                      className="compact"
                      onClick={() => void approve(item.id)}
                    >
                      Aprovar
                    </button>
                  ) : executable(item) ? (
                    <button
                      className="compact"
                      onClick={() => void execute(item)}
                    >
                      Executar erasure
                    </button>
                  ) : (
                    "Sem ação automática"
                  )}
                </span>
              </div>
            ))}
            {!items.length && <Empty text="Nenhuma solicitação registrada." />}
          </div>
        )}
      </section>
    </>
  );
}
function Help() {
  return (
    <>
      <div className="page-head">
        <div>
          <p className="eyebrow">OBSERVABILITY BASICS</p>
          <h1>Decida sem decorar jargões</h1>
          <p>Definições curtas conectadas ao impacto operacional.</p>
        </div>
      </div>
      <div className="glossary">
        {glossary.map(([term, definition]) => (
          <article key={term}>
            <div className="glossary-icon">
              <BookOpen />
            </div>
            <h2>{term}</h2>
            <p>{definition}</p>
            <a
              href="https://sre.google/sre-book/service-level-objectives/"
              target="_blank"
              rel="noreferrer"
            >
              Ver contexto <ChevronRight size={15} />
            </a>
          </article>
        ))}
      </div>
    </>
  );
}
function Empty({ text }: { text: string }) {
  return (
    <div className="empty">
      <Boxes />
      <p>{text}</p>
    </div>
  );
}
