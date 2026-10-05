#!/usr/bin/env python3
"""Smoke test real de subpaths; nunca imprime senhas, tokens ou cookies."""
import http.cookiejar
import json
import pathlib
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = 'https://sentinelops.tqi.com.br'
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
results = {}

def call(path, body=None, headers=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(BASE + path, data=data, headers={
        'Content-Type': 'application/json', 'Origin': BASE, **(headers or {})})
    try:
        response = client.open(request, timeout=20)
        return response.status, response.read(), response.url
    except urllib.error.HTTPError as error:
        return error.code, b'', error.url

def record(name, ok, **details):
    results[name] = {'passed': bool(ok), **details}

grafana = json.loads(subprocess.check_output(['docker', 'inspect', 'sentinelops-grafana-1']))[0]
environment = dict(item.split('=', 1) for item in grafana['Config']['Env'])
status, _, _ = call('/grafana/login', {'user': environment['GF_SECURITY_ADMIN_USER'],
                                    'password': environment['GF_SECURITY_ADMIN_PASSWORD']})
record('grafana_login', status == 200, http=status,
       session_cookie_root=any(c.name == 'grafana_session' and c.path == '/' for c in jar))

for service, path in [('portal', '/'), ('sentinelops', '/sentinelops/'),
                      ('grafana', '/grafana/'), ('prometheus', '/prometheus/'),
                      ('temporal', '/temporal/'), ('keycloak', '/keycloak/admin/master/console/')]:
    status, body, final = call(path)
    record(service + '_html', status == 200 and urllib.parse.urlparse(final).path.startswith(path), http=status)
    text = body.decode(errors='replace')
    assets = re.findall(r'(?:src|href)=["\']([^"\']+\.(?:js|css)(?:\?[^"\']*)?)["\']', text)
    failures = []
    for asset in assets[:8]:
        full = urllib.parse.urljoin(final, asset)
        if not full.startswith(BASE + '/'): continue
        asset_path = full[len(BASE):]
        asset_status, _, _ = call(asset_path)
        if asset_status != 200: failures.append({'path': asset_path, 'http': asset_status})
    record(service + '_assets', not failures and (bool(assets) or service == 'portal'), checked=min(len(assets), 8), failures=failures)

status, body, _ = call('/grafana/api/datasources')
sources = json.loads(body) if status == 200 else []
record('datasources', status == 200 and {'loki', 'tempo', 'pyroscope', 'prometheus'}.issubset({x['uid'] for x in sources}), http=status, count=len(sources))

status, body, _ = call('/prometheus/api/v1/query?query=count(up)')
data = json.loads(body) if status == 200 else {}
samples = data.get('data', {}).get('result', [])
record('real_metrics', status == 200 and bool(samples), http=status,
       targets=int(float(samples[0]['value'][1])) if samples else 0)
status, body, _ = call('/loki/api/v1/labels')
data = json.loads(body) if status == 200 else {}
record('loki_read', status == 200, http=status, labels=len(data.get('data', [])))
status, body, _ = call('/tempo/api/search?limit=1')
data = json.loads(body) if status == 200 else {}
record('tempo_read', status == 200, http=status, traces=len(data.get('traces', [])))
end = int(time.time() * 1000)
status, body, _ = call('/pyroscope/querier.v1.QuerierService/LabelNames', {'start': end - 3600000, 'end': end}, {'Connect-Protocol-Version': '1'})
data = json.loads(body) if status == 200 else {}
record('pyroscope_query', status == 200, http=status, labels=len(data.get('names', [])))
for path in ['/loki/api/v1/push', '/prometheus/api/v1/write', '/tempo/api/traces', '/temporal/']:
    status, _, _ = call(path, {})
    record('write_block_' + path, status == 403, http=status)

for path in ['/loki/api/v1/labels', '/tempo/api/search', '/pyroscope/querier.v1.QuerierService/LabelNames']:
    try:
        response = urllib.request.urlopen(BASE + path, timeout=10)
        status = response.status
    except urllib.error.HTTPError as error:
        status = error.code
    record('unauthenticated_' + path, status == 401, http=status)

status, body, _ = call('/keycloak/realms/master/.well-known/openid-configuration')
data = json.loads(body) if status == 200 else {}
record('keycloak_issuer', status == 200 and data.get('issuer') == BASE + '/keycloak/realms/master', http=status)

local = {}
for line in pathlib.Path('/opt/sentinelops/.env').read_text().splitlines():
    if line.strip() and not line.lstrip().startswith('#') and '=' in line:
        key, value = line.split('=', 1)
        if key in {'LOCAL_ADMIN_USER', 'LOCAL_ADMIN_PASSWORD'}:
            local[key] = value.strip().strip('"\'')
status, body, _ = call('/sentinelops/api/v1/auth/login', {'username': local.get('LOCAL_ADMIN_USER', 'admin'),
                                                     'password': local.get('LOCAL_ADMIN_PASSWORD', '')})
data = json.loads(body) if status == 200 else {}
token = data.get('data', {}).get('accessToken', '')
record('sentinelops_login', status == 200 and bool(token), http=status)
if token:
    status, body, _ = call('/sentinelops/api/v1/services', headers={'Authorization': 'Bearer ' + token})
    data = json.loads(body) if status == 200 else {}
    record('sentinelops_catalog', status == 200, http=status, services=len(data.get('data', [])))

print(json.dumps(results, indent=2))
raise SystemExit(0 if all(x['passed'] for x in results.values()) else 1)
