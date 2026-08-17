import { describe, expect, it, vi } from 'vitest'
import { API } from './api'

describe('API', () => {
  it('surfaces the request id on failures', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'forbidden', message: 'sem acesso', requestId: 'req-1' } }), { status: 403, headers: { 'Content-Type': 'application/json' } })))
    await expect(new API('token').services()).rejects.toThrow('req-1')
  })
})

