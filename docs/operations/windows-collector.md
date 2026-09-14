# Collector Windows / AD

## Estado e fronteira

O pacote em `deploy/agents/windows` instala o Grafana Alloy como serviço Windows com métricas do `prometheus.exporter.windows` e eventos Application/System. A execução no Windows, a chegada ao gateway e os alertas continuam pendentes de um host TQI de homologação autorizado. Este pacote não faz descoberta de rede, não altera roles, não reinicia serviços de negócio e não baixa binários de URLs arbitrárias.

O Alloy suporta `prometheus.exporter.windows` para coletar métricas de host e `loki.source.windowsevent` para os Event Logs; `bookmark_path` persiste a posição de leitura. Consulte a documentação oficial de [monitoramento Windows](https://grafana.com/docs/alloy/latest/monitor/monitor-windows/) e do [source de Event Log](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.source.windowsevent/) antes de mudanças de versão.

## Pré-requisitos

- Windows Server suportado, PowerShell 5.1+ e execução elevada.
- Instalador Alloy aprovado, transferido por canal autenticado, e o SHA-256 correspondente. O script exige ambos e registra apenas o hash.
- Um certificado mTLS exclusivo do host, sua CA e endpoints HTTPS do gateway. A chave é copiada para `%ProgramData%\SentinelOps\Alloy\certs` com ACL restrita ao `SYSTEM`, administradores e à identidade do serviço.
- Saída do host apenas aos endpoints de métricas e logs aprovados. A interface administrativa permanece em `127.0.0.1:12345`.

Por padrão, o serviço usa `NT AUTHORITY\LocalService` e o instalador adiciona essa identidade aos grupos locais `Event Log Readers` e `Performance Monitor Users`. Para conta de domínio, use o processo de gestão de identidades da TQI e o direito **Log on as a service**; não passe senha de serviço em linha de comando. As permissões requeridas e a recomendação de restringir o listener estão detalhadas na [documentação de permissões do Alloy](https://grafana.com/docs/grafana-cloud/observe-and-act/send-data/alloy/access_permissions/windows/).

## Instalação em homologação

Execute inicialmente apenas o preflight:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-windows-collector.ps1 -Phase preflight
```

Após associar host, site, time e certificado à mudança aprovada, execute a instalação. `-EnablePack` só aceita um pack quando o serviço/role local correspondente está presente; isso evita habilitar coletores AD, IIS, SQL Server, DNS ou DHCP em host incompatível.

```powershell
.\scripts\install-windows-collector.ps1 -Phase all `
  -AlloyInstaller C:\Staging\alloy-installer-windows-amd64.exe `
  -AlloyInstallerSha256 '<sha256-publicado-do-artefato>' `
  -HostName win-hml-01 -Environment hml -Team plataforma -Location dc-sp `
  -MetricsEndpoint https://ingest.example.invalid/prometheus/api/v1/write `
  -LogsEndpoint https://ingest.example.invalid/loki/loki/api/v1/push `
  -TlsServerName ingest.example.invalid `
  -CaFile C:\Staging\ca.crt `
  -ClientCertificateFile C:\Staging\client.crt `
  -ClientKeyFile C:\Staging\client.key
```

`-IncludeSecurityEventLog` é opt-in. Quando habilitado, o template exclui mensagem, event data e user data; a revisão de privacidade e da política AD é obrigatória antes de promover esse canal. A configuração padrão coleta Application/System, já removendo event/user data e descartando linhas com padrões de segredo. Isso não é uma garantia de anonimização: revise fontes e retenção no gateway antes do rollout.

O comando usa o modo silencioso suportado pelo instalador Alloy e chama `alloy validate` antes de declarar o serviço válido. A sintaxe e os limites de `validate` constam na [referência oficial da CLI](https://grafana.com/docs/alloy/latest/reference/cli/).

No desenvolvimento, `make harness-check` também interpreta a sintaxe PowerShell em imagem Microsoft fixada por digest. Isso prova a parseabilidade do script, não a execução de cmdlets, permissões ou coletores em Windows.

## Verificação e critérios de aceite

```powershell
.\scripts\install-windows-collector.ps1 -Phase verify
Get-Service Alloy
```

O verify confirma a configuração, o serviço e readiness local. Depois, no gateway autorizado, correlacione a fonte com host/site/time e faça os quatro testes da SPEC-007: parada controlada de serviço, restart com bookmark, negação de permissão visível e jornada AD além de bind/porta. HTTP 200 ou o serviço `Running` isoladamente não aprovam o host.

## Upgrade, rollback e remoção

O instalador preserva `%ProgramData%\SentinelOps\Alloy\config.previous.alloy` e os bookmarks. Se o novo conteúdo de configuração falhar após a instalação, restaure somente a configuração com:

```powershell
.\scripts\install-windows-collector.ps1 -Phase rollback
```

Rollback binário exige o instalador anterior verificado e a janela de mudança; o script não inventa nem baixa uma versão anterior. Para remover apenas o serviço, preservando certificados, WAL e bookmarks para investigação:

```powershell
.\scripts\install-windows-collector.ps1 -Phase remove -ConfirmRemove
```

Não apague `%ProgramData%\SentinelOps\Alloy` sem registrar a decisão, pois isso elimina o checkpoint de eventos e a evidência local.
