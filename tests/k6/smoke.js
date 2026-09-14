import http from 'k6/http'
import { check, sleep } from 'k6'
import { Rate } from 'k6/metrics'

export const errorRate = new Rate('sentinel_smoke_errors')
export const options = { vus: 2, duration: '15s', thresholds: { http_req_failed: ['rate<0.01'], http_req_duration: ['p(95)<500'], sentinel_smoke_errors: ['rate<0.01'] } }
const base = __ENV.SENTINELOPS_API_URL || 'http://api:8080'
export default function () {
  const health = http.get(`${base}/healthz`, { timeout: '10s' })
  const ready = http.get(`${base}/readyz`, { timeout: '10s' })
  const ok = check(health, { 'health status 200': response => response.status === 200 }) &&
    check(ready, { 'readiness status 200': response => response.status === 200 })
  errorRate.add(!ok); sleep(0.2)
}
export function handleSummary(data) { return { '/artifacts/k6-summary.json': JSON.stringify(data, null, 2), stdout: JSON.stringify({ checks: data.metrics.checks, http_req_duration: data.metrics.http_req_duration }, null, 2) } }
