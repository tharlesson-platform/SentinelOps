export type Service = { id: string; name: string; displayName: string; description: string; ownerTeam: string; tier: string; labels: Record<string, string> }
export type Asset = { id: string; assetId: string; name: string; kind: string; site: string; ownerTeam: string; environment: string; lifecycle: 'active' | 'stale' | 'retired'; source: string; lastSeen?: string; labels: Record<string, string> }
export type AssetSearch = { items: Asset[]; nextCursor?: string; query: string; limit: number }
export type Agent = { id: string; name: string; region: string; location: string; environment: string; status: 'online' | 'offline'; lastHeartbeat?: string }
export type Scenario = { id: string; name: string; serviceRef: string; environment: string; type: string; enabled: boolean; version: number; spec: Record<string, unknown> }
export type SyntheticRun = { id: string; scenarioId: string; scenario: string; status: 'PASS' | 'FAIL' | 'INCONCLUSIVE' | 'RUNNING'; startedAt?: string; finishedAt?: string; result: Record<string, unknown> }
export type DataLifecycleDomain = 'control-plane-metadata' | 'operational-evidence' | 'telemetry-references'
export type DataLifecycleRequest = { id: string; requestType: 'export' | 'erasure'; status: 'requested' | 'approved' | 'running' | 'completed' | 'rejected' | 'failed'; requestedBy: string; approvedBy?: string; reason: string; scope: { subjectRef: string; domains: DataLifecycleDomain[] }; evidence: Record<string, unknown>; createdAt: string; updatedAt: string; completedAt?: string }
export type DataLifecycleRequestInput = { requestType: 'export' | 'erasure'; reason: string; scope: { subjectRef: string; domains: DataLifecycleDomain[] } }
export type SourceStatus = { state: 'available' | 'partial' | 'no_data' | 'unavailable'; message?: string; fetchedAt: string }
export type HostSummary = { name: string; instance: string; os?: string; kernel?: string; architecture?: string; environment?: string; provider?: string; cpuPercent: number | null; memoryPercent: number | null; diskPercent: number | null; up: boolean | null; state: 'up' | 'down' | 'telemetry_unknown' }
export type ContainerSummary = { name: string; id?: string; image?: string; host: string; service?: string; environment?: string; lastSeen: string; cpuPercent: number | null; memoryBytes: number | null; memoryLimitBytes: number | null; memoryPercent: number | null; restarts24h: number | null; state: 'running' | 'stale' }
export type TelemetryPoint = { timestamp: string; value: number }
export type TelemetrySeries = { labels: Record<string, string>; points: TelemetryPoint[] }
export type HostDetails = { host: string; start: string; end: string; stepSeconds: number; metrics: Record<string, TelemetrySeries[]>; sources: { prometheus: SourceStatus } }
export type ContainerDetails = { container: string; host: string; start: string; end: string; stepSeconds: number; metrics: Record<string, TelemetrySeries[]>; sources: { prometheus: SourceStatus } }
export type LogEntry = { timestamp: string; line: string; labels: Record<string, string> }
export type TraceSummary = { traceId: string; rootServiceName: string; rootTraceName: string; startTimeUnixNano: string; durationMs: number }
export type APMService = { name: string; environment?: string; host?: string; requestsPerSecond: number | null; errorPercent: number | null; p95Seconds: number | null; p99Seconds: number | null; telemetryState: string }
export type TelemetryList<T> = { items: T[]; sources: Record<string, SourceStatus> }
export type ObservabilityOverview = { values: Record<string, number | null>; sources: { prometheus: SourceStatus } }

type Envelope<T> = { data?: T; error?: { code: string; message: string; requestId: string } }

export class API {
  constructor(private token: string, private readonly base = '') {}
  setToken(token: string) { this.token = token }
  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(`${this.base}${path}`, {
      ...init,
      headers: { 'Content-Type': 'application/json', ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}), ...init?.headers },
    })
    const payload = (await response.json()) as Envelope<T>
    if (!response.ok || payload.error) throw new Error(payload.error ? `${payload.error.message} (${payload.error.requestId})` : `HTTP ${response.status}`)
    return payload.data as T
  }
  login(username: string, password: string) { return this.request<{ accessToken: string }>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }) }
  services() { return this.request<Service[]>('/api/v1/services') }
  searchAssets(query = '', cursor = '', limit = 25) {
    const params = new URLSearchParams({ limit: String(limit) })
    if (query.trim()) params.set('q', query.trim())
    if (cursor) params.set('cursor', cursor)
    return this.request<AssetSearch>(`/api/v1/assets/search?${params.toString()}`)
  }
  agents() { return this.request<Agent[]>('/api/v1/agents') }
  scenarios() { return this.request<Scenario[]>('/api/v1/scenarios') }
  syntheticRuns() { return this.request<SyntheticRun[]>('/api/v1/synthetic-runs?limit=50') }
  saveScenario(body: Omit<Scenario, 'id' | 'version' | 'enabled'>) { return this.request<Scenario>('/api/v1/scenarios', { method: 'POST', body: JSON.stringify(body) }) }
  dataLifecycleRequests() { return this.request<DataLifecycleRequest[]>('/api/v1/data-lifecycle-requests') }
  createDataLifecycleRequest(body: DataLifecycleRequestInput) { return this.request<DataLifecycleRequest>('/api/v1/data-lifecycle-requests', { method: 'POST', body: JSON.stringify(body) }) }
  approveDataLifecycleRequest(id: string) { return this.request<DataLifecycleRequest>(`/api/v1/data-lifecycle-requests/${encodeURIComponent(id)}/approve`, { method: 'POST' }) }
  executeDataLifecycleRequest(id: string) { return this.request<DataLifecycleRequest>(`/api/v1/data-lifecycle-requests/${encodeURIComponent(id)}/execute`, { method: 'POST', body: JSON.stringify({ confirm: 'ERASE_CONTROL_PLANE_METADATA' }) }) }
  observabilityOverview() { return this.request<ObservabilityOverview>('/api/v1/observability/overview') }
  hosts(environment = '') { const params = new URLSearchParams(); if (environment) params.set('environment', environment); return this.request<TelemetryList<HostSummary>>(`/api/v1/observability/hosts${params.size ? `?${params}` : ''}`) }
  hostDetails(host: string, window = '1h') { const params = telemetryWindow(window); return this.request<HostDetails>(`/api/v1/observability/hosts/${encodeURIComponent(host)}?${params}`) }
  containers(filters: { host?: string; service?: string } = {}) { const params = new URLSearchParams(); if (filters.host) params.set('host', filters.host); if (filters.service) params.set('service', filters.service); return this.request<TelemetryList<ContainerSummary>>(`/api/v1/observability/containers${params.size ? `?${params}` : ''}`) }
  containerDetails(container: string, host = '', window = '1h') { const params = telemetryWindow(window); if (host) params.set('host', host); return this.request<ContainerDetails>(`/api/v1/observability/containers/${encodeURIComponent(container)}?${params}`) }
  logs(filters: { host?: string; containerId?: string; service?: string; severity?: string; search?: string; window?: string; limit?: number } = {}) { const params = telemetryWindow(filters.window || '15m'); for (const [key, value] of Object.entries(filters)) if (value !== undefined && key !== 'window' && value !== '') params.set(key, String(value)); return this.request<TelemetryList<LogEntry> & { query: string; start: string; end: string }>(`/api/v1/observability/logs?${params}`) }
  apm() { return this.request<TelemetryList<APMService>>('/api/v1/observability/apm') }
  traces(filters: { host?: string; container?: string; service?: string; error?: boolean; window?: string; limit?: number } = {}) { const params = telemetryWindow(filters.window || '1h'); for (const [key, value] of Object.entries(filters)) if (value !== undefined && key !== 'window' && value !== '') params.set(key, String(value)); return this.request<TelemetryList<TraceSummary> & { query: string; start: string; end: string }>(`/api/v1/observability/traces?${params}`) }
}

function telemetryWindow(value: string) {
  const durations: Record<string, number> = { '15m': 15 * 60, '1h': 60 * 60, '6h': 6 * 60 * 60, '24h': 24 * 60 * 60 }
  const seconds = durations[value] || durations['1h']
  const end = Math.floor(Date.now() / 1000)
  return new URLSearchParams({ start: String(end - seconds), end: String(end) })
}
