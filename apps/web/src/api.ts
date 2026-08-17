export type Service = { id: string; name: string; displayName: string; description: string; ownerTeam: string; tier: string; labels: Record<string, string> }
export type Agent = { id: string; name: string; region: string; location: string; environment: string; status: 'online' | 'offline'; lastHeartbeat?: string }
export type Scenario = { id: string; name: string; serviceRef: string; environment: string; type: string; enabled: boolean; version: number; spec: Record<string, unknown> }

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
  agents() { return this.request<Agent[]>('/api/v1/agents') }
  scenarios() { return this.request<Scenario[]>('/api/v1/scenarios') }
  saveScenario(body: Omit<Scenario, 'id' | 'version' | 'enabled'>) { return this.request<Scenario>('/api/v1/scenarios', { method: 'POST', body: JSON.stringify(body) }) }
}

