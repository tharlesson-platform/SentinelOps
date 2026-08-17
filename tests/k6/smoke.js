import http from 'k6/http'
import { check, sleep } from 'k6'
import { Rate } from 'k6/metrics'

export const errorRate = new Rate('sentinel_smoke_errors')
export const options = { vus: 2, duration: '15s', thresholds: { http_req_failed: ['rate<0.01'], http_req_duration: ['p(95)<500'], sentinel_smoke_errors: ['rate<0.01'] } }
const base = __ENV.DEMO_URL || 'http://demo-api:8090'
export default function () {
  const response = http.get(`${base}/api/orders`, { headers: { 'X-Synthetic-Test': 'sentinelops-k6' }, timeout: '10s' })
  const ok = check(response, { 'status 200': r => r.status === 200, 'version present': r => Boolean(r.json('version')) })
  errorRate.add(!ok); sleep(0.2)
}
export function handleSummary(data) { return { '/artifacts/k6-summary.json': JSON.stringify(data, null, 2), stdout: JSON.stringify({ checks: data.metrics.checks, http_req_duration: data.metrics.http_req_duration }, null, 2) } }

