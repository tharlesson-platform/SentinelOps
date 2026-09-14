import { afterEach, describe, expect, it, vi } from 'vitest'
import { API } from './api'

describe('API', () => {
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })
  it('surfaces the request id on failures', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'forbidden', message: 'sem acesso', requestId: 'req-1' } }), { status: 403, headers: { 'Content-Type': 'application/json' } })))
    await expect(new API('token').services()).rejects.toThrow('req-1')
  })

  it('uses the bounded asset search endpoint with encoded query', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: { items: [], query: 'vm edge', limit: 25 } }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await new API('token').searchAssets(' vm edge ')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/assets/search?limit=25&q=vm+edge', expect.anything())
  })

  it('uses the tenant-scoped data lifecycle routes', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await new API('token').dataLifecycleRequests()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/data-lifecycle-requests', expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer token' }) }))
  })

  it('sends the explicit control-plane erasure confirmation', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: {} }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await new API('token').executeDataLifecycleRequest('request-1')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/data-lifecycle-requests/request-1/execute', expect.objectContaining({ method: 'POST', body: JSON.stringify({ confirm: 'ERASE_CONTROL_PLANE_METADATA' }) }))
  })

  it('preserves host and container context in telemetry queries', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-14T12:00:00Z'))
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: { items: [], sources: {} } }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await new API('token').logs({ host: 'easy-vm', containerId: 'abc123', severity: 'ERROR', window: '15m', limit: 300 })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/observability/logs?start=1789386300&end=1789387200&host=easy-vm&containerId=abc123&severity=ERROR&limit=300', expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer token' }) }))
  })

  it('encodes the selected host in native metric detail requests', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-14T12:00:00Z'))
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: { metrics: {}, sources: {} } }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await new API('token').hostDetails('host edge/01', '1h')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/observability/hosts/host%20edge%2F01?start=1789383600&end=1789387200', expect.anything())
  })
})
