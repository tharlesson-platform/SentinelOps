import { useEffect, useState } from "react";
import { API, APMService, LogEntry, TraceSummary } from "./api";
import { EntityContext, Route } from "./navigation";
import { QueryStatus, useQuery } from "./Observability";

type Props = {
  api: API;
  route: Route;
  refresh: number;
  update: (value: Partial<Route>) => void;
};
type Choice = { label: string; context: EntityContext };
const key = (c: EntityContext) =>
  JSON.stringify(
    Object.entries(c)
      .filter(([, v]) => v)
      .sort(),
  );

function Choices({
  choices,
  context,
  onChange,
  empty = "Escolha um recurso",
}: {
  choices: Choice[];
  context: EntityContext;
  onChange: (c: EntityContext) => void;
  empty?: string;
}) {
  const [search, setSearch] = useState("");
  const selected = key(context);
  const options = [
    ...new Map(choices.map((c) => [key(c.context), c])).values(),
  ];
  const current = options.find((c) => key(c.context) === selected);
  return (
    <div className="resource-controls">
      <label>
        Localizar recurso
        <input
          aria-label="Localizar recurso"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Digite parte do nome"
        />
      </label>
      <label>
        Recurso
        <select
          aria-label="Recurso"
          value={selected === "[]" ? "" : selected}
          onChange={(e) => {
            if (e.target.value === "") onChange({});
            else {
              const choice = options.find(
                (c) => key(c.context) === e.target.value,
              );
              if (choice) onChange(choice.context);
            }
          }}
        >
          <option value="">{empty}</option>
          {selected !== "[]" && !current && (
            <option value={selected}>
              Seleção atual ·{" "}
              {Object.values(context).filter(Boolean).join(" · ")} (fora da
              lista)
            </option>
          )}
          {options
            .filter(
              (c) =>
                key(c.context) === selected ||
                c.label
                  .toLocaleLowerCase()
                  .includes(search.toLocaleLowerCase()),
            )
            .map((c) => (
              <option key={key(c.context)} value={key(c.context)}>
                {c.label}
              </option>
            ))}
        </select>
      </label>
    </div>
  );
}
export function MetricResourceSelector({ api, route, refresh, update }: Props) {
  const hosts = useQuery(
    `metric-host-options:${refresh}:${route.end}`,
    (signal) => api.hosts("", signal),
  );
  const containers = useQuery(
    `metric-container-options:${refresh}:${route.end}`,
    (signal) => api.containers({}, signal),
  );
  const choices: Choice[] = [
    ...(hosts.data?.items || []).map((h) => ({
      label: `Servidor · ${h.hostName || h.instance}`,
      context: { host: h.instance, hostName: h.hostName },
    })),
    ...(containers.data?.items || []).map((c) => ({
      label: `Container · ${c.name} · ${c.hostName || c.host}`,
      context: {
        host: c.host,
        hostName: c.hostName,
        container: c.name,
        containerId: c.logContainerId,
      },
    })),
  ];
  return (
    <section
      className="resource-selector"
      aria-label="Selecionar recurso de métricas"
    >
      <h2>Qual recurso você quer acompanhar?</h2>
      <p>
        Escolha um servidor ou container. O nome do servidor diferencia
        containers com o mesmo nome.
      </p>
      <Choices
        choices={choices}
        context={route.context}
        onChange={(context) => update({ context })}
      />
      <QueryStatus
        query={hosts}
        source={hosts.data?.sources.prometheus}
        label="Lista de servidores"
      />
      <QueryStatus
        query={containers}
        source={containers.data?.sources.prometheus}
        label="Lista de containers"
      />
    </section>
  );
}

export function APMResourceSelector({
  items,
  route,
  update,
}: {
  items: APMService[];
  route: Route;
  update: Props["update"];
}) {
  return (
    <section
      className="resource-selector"
      aria-label="Selecionar aplicação APM"
    >
      <h2>Qual aplicação você quer acompanhar?</h2>
      <p>
        Aplicações que emitiram métricas, identificadas por serviço, ambiente e
        servidor.
      </p>
      <Choices
        choices={items.map((i) => ({
          label: `${i.name} · ${i.environment || "Ambiente não informado"} · ${i.host || "Servidor não informado"}`,
          context: {
            service: i.name,
            apmResource: JSON.stringify([
              i.name,
              i.environment || "",
              i.host || "",
            ]),
            serviceEnvironment: i.environment,
            hostName: i.host,
          },
        }))}
        context={route.context}
        onChange={(context) => update({ context })}
        empty="Todas as aplicações"
      />
    </section>
  );
}

export function SignalResourceSelector({
  api,
  route,
  refresh,
  update,
  kind,
  observed,
}: Props & {
  kind: "logs" | "traces";
  observed?: LogEntry[] | TraceSummary[];
}) {
  // Discovery is a separate, explicitly requested bounded search. It never changes the active scope.
  const [discover, setDiscover] = useState(false);
  const discovery = useQuery(
    `signal-options:${kind}:${route.window}:${route.end}:${refresh}`,
    async (signal) =>
      kind === "logs"
        ? await api.logs(
            { window: route.window, end: route.end, limit: 300 },
            signal,
          )
        : await api.traces(
            { window: route.window, end: route.end, limit: 100 },
            signal,
          ),
    discover,
  );
  const [known, setKnown] = useState<{ window: string; choices: Choice[] }>({
    window: "",
    choices: [],
  });
  const windowKey = `${kind}:${route.window}:${route.end}:${refresh}`;
  const entries = discovery.data?.items || observed;
  useEffect(() => {
    if (!entries) return;
    const choices: Choice[] = [];
    for (const entry of entries) {
      if ("labels" in entry) {
        const {
          host_name: host,
          service_name: service,
          container_id: id,
          container_name: name,
        } = entry.labels;
        if (host)
          choices.push({
            label: `Servidor · ${host}`,
            context: { hostName: host },
          });
        if (service)
          choices.push({
            label: `Aplicação · ${service}${host ? ` · ${host}` : ""}`,
            context: { service, hostName: host },
          });
        if (id)
          choices.push({
            label: `Container · ${name || id} ${host ? `· ${host}` : ""}`,
            context: { hostName: host, container: name, containerId: id },
          });
      } else if (entry.rootServiceName)
        choices.push({
          label: `Aplicação · ${entry.rootServiceName}`,
          context: { service: entry.rootServiceName },
        });
    }
    setKnown((previous) => ({
      window: windowKey,
      choices: [
        ...new Map(
          [
            ...(previous.window === windowKey ? previous.choices : []),
            ...choices,
          ].map((c) => [key(c.context), c]),
        ).values(),
      ],
    }));
  }, [entries, windowKey]);
  const c = route.context;
  const [draft, setDraft] = useState({
    host: c.hostName || "",
    service: c.service || "",
    container: (kind === "logs" ? c.containerId : c.container) || "",
  });
  useEffect(
    () =>
      setDraft({
        host: c.hostName || "",
        service: c.service || "",
        container: (kind === "logs" ? c.containerId : c.container) || "",
      }),
    [c, kind],
  );
  return (
    <section
      className="resource-selector"
      aria-label="Selecionar recurso da fonte"
    >
      <h2>Qual recurso você quer investigar?</h2>
      <p>
        Escolha uma origem observada em {kind === "logs" ? "Loki" : "Tempo"}. A
        lista usa até {kind === "logs" ? "300 eventos" : "100 requisições"} do
        período e pode não incluir todos os recursos.
      </p>
      <Choices
        choices={known.window === windowKey ? known.choices : []}
        context={c}
        onChange={(context) => update({ context })}
        empty="Todas as fontes"
      />
      <button
        type="button"
        onClick={() => {
          setDiscover(true);
          if (discover) discovery.retry();
        }}
      >
        Listar recursos de todas as fontes
      </button>
      <small>
        Atualiza apenas as opções acima; mantém os filtros da investigação.
      </small>
      {discover && (
        <QueryStatus
          query={discovery}
          source={discovery.data?.sources[kind === "logs" ? "loki" : "tempo"]}
          label="Descoberta de recursos"
        />
      )}
      <details>
        <summary>Selecionar por nome ou combinar filtros</summary>
        <p>
          Informe os nomes emitidos pela fonte. Campos vazios não restringem a
          busca; ao aplicar, estes campos substituem a seleção anterior.
        </p>
        <form
          className="resource-controls"
          onSubmit={(e) => {
            e.preventDefault();
            const container = draft.container.trim();
            update({
              context: {
                hostName: draft.host.trim() || undefined,
                service: draft.service.trim() || undefined,
                container:
                  kind === "traces" ? container || undefined : undefined,
                ...(kind === "logs"
                  ? { containerId: container || undefined }
                  : {}),
              },
            });
          }}
        >
          <label>
            Servidor na fonte
            <input
              aria-label="Servidor na fonte"
              value={draft.host}
              onChange={(e) => setDraft({ ...draft, host: e.target.value })}
              placeholder={kind === "logs" ? "host_name" : "host.name"}
            />
          </label>
          <label>
            Aplicação na fonte
            <input
              aria-label="Aplicação na fonte"
              value={draft.service}
              onChange={(e) => setDraft({ ...draft, service: e.target.value })}
              placeholder="service.name"
            />
          </label>
          <label>
            {kind === "logs"
              ? "ID do container nos logs"
              : "Container na requisição"}
            <input
              aria-label="Container na fonte"
              value={draft.container}
              onChange={(e) =>
                setDraft({ ...draft, container: e.target.value })
              }
            />
          </label>
          <button>Aplicar recurso</button>
        </form>
      </details>
    </section>
  );
}
