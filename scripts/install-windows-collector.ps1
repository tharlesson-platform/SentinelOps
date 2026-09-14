#requires -Version 5.1
[CmdletBinding()]
param(
  [ValidateSet('preflight', 'configure', 'deploy', 'verify', 'rollback', 'remove', 'all')]
  [string]$Phase = 'all',
  [string]$AlloyInstaller,
  [ValidatePattern('^[A-Fa-f0-9]{64}$')]
  [string]$AlloyInstallerSha256,
  [string]$HostName = $env:COMPUTERNAME,
  [string]$Environment = 'development',
  [string]$Team = 'platform',
  [string]$Location = 'on-premises',
  [string]$MetricsEndpoint,
  [string]$LogsEndpoint,
  [string]$TlsServerName,
  [string]$CaFile,
  [string]$ClientCertificateFile,
  [string]$ClientKeyFile,
  [ValidateSet('AD', 'IIS', 'SQLServer', 'DNS', 'DHCP')]
  [string[]]$EnablePack = @(),
  [switch]$IncludeSecurityEventLog,
  [ValidateSet('LocalService', 'NetworkService')]
  [string]$ServiceIdentity = 'LocalService',
  [string]$InstallRoot = "$env:ProgramFiles\GrafanaLabs\Alloy",
  [string]$DataRoot = "$env:ProgramData\SentinelOps\Alloy",
  [switch]$ConfirmRemove
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepositoryRoot = Split-Path -Parent $PSScriptRoot
$TemplateRoot = Join-Path $RepositoryRoot 'deploy\agents\windows'
$ServiceName = 'Alloy'
$BaseCollectors = @('cpu', 'logical_disk', 'memory', 'net', 'os', 'physical_disk', 'process', 'service', 'system', 'tcp', 'time')
$PackCollectors = @{ AD = 'ad'; IIS = 'iis'; SQLServer = 'mssql'; DNS = 'dns'; DHCP = 'dhcp' }

function Fail([string]$Message) { throw "[sentinelops][windows-collector] $Message" }
function Log([string]$Message) { Write-Host "[sentinelops][windows-collector] $Message" }
function PhaseSelected([string]$Wanted) { return $Phase -eq 'all' -or $Phase -eq $Wanted }

function Assert-Administrator {
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = [Security.Principal.WindowsPrincipal]::new($identity)
  if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Fail 'execute o PowerShell elevado; o instalador altera serviço, ACLs e grupos locais.'
  }
}

function Assert-Name([string]$Value, [string]$Field) {
  if ([string]::IsNullOrWhiteSpace($Value) -or $Value -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$') {
    Fail "$Field inválido; use somente letras, números, ponto, hífen ou sublinhado (máximo 63 caracteres)."
  }
}

function Assert-HttpsEndpoint([string]$Value, [string]$Field) {
  if ([string]::IsNullOrWhiteSpace($Value)) { Fail "$Field é obrigatório." }
  try { $uri = [Uri]$Value } catch { Fail "$Field não é uma URL válida." }
  if ($uri.Scheme -ne 'https' -or -not $uri.Host -or $uri.UserInfo -or $uri.Query -or $uri.Fragment) {
    Fail "$Field deve ser HTTPS, sem credencial, query ou fragmento."
  }
}

function Assert-File([string]$Path, [string]$Field) {
  if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) { Fail "$Field ausente." }
  if ((Get-Item -LiteralPath $Path).Length -le 0) { Fail "$Field está vazio." }
}

function Get-ServicePrincipal {
  if ($ServiceIdentity -eq 'LocalService') { return 'NT AUTHORITY\LocalService' }
  return 'NT AUTHORITY\NetworkService'
}

function Add-CollectorGroup([string]$GroupName, [string]$Principal) {
  try { Add-LocalGroupMember -Group $GroupName -Member $Principal -ErrorAction Stop } catch {
    $message = $_.Exception.Message
    if ($message -notmatch 'already a member|já é membro') { throw }
  }
}

function Protect-CollectorData([string]$Principal) {
  foreach ($path in @($DataRoot, (Join-Path $DataRoot 'certs'), (Join-Path $DataRoot 'bookmarks'))) {
    New-Item -ItemType Directory -Force -Path $path | Out-Null
    & icacls.exe $path /inheritance:r /grant:r 'SYSTEM:(OI)(CI)F' "${Principal}:(OI)(CI)R" 'Administrators:(OI)(CI)F' | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "não foi possível aplicar ACL em $path." }
  }
}

function Test-PackPrerequisite([string]$Pack) {
  $exists = switch ($Pack) {
    'AD' { $null -ne (Get-Service -Name 'NTDS' -ErrorAction SilentlyContinue) }
    'IIS' { $null -ne (Get-Service -Name 'W3SVC' -ErrorAction SilentlyContinue) }
    'SQLServer' { $null -ne (Get-Service -ErrorAction SilentlyContinue | Where-Object { $_.Name -eq 'MSSQLSERVER' -or $_.Name -like 'MSSQL$*' } | Select-Object -First 1) }
    'DNS' { $null -ne (Get-Service -Name 'DNS' -ErrorAction SilentlyContinue) }
    'DHCP' { $null -ne (Get-Service -Name 'DHCPServer' -ErrorAction SilentlyContinue) }
  }
  if (-not $exists) { Fail "pack $Pack solicitado, mas o serviço/role correspondente não foi encontrado; não habilite collectors que o host não suporta." }
}

function Copy-CollectorSecret([string]$Source, [string]$Destination) {
  Copy-Item -LiteralPath $Source -Destination $Destination -Force
  & icacls.exe $Destination /inheritance:r /grant:r 'SYSTEM:F' "$(Get-ServicePrincipal):R" 'Administrators:F' | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail "não foi possível proteger $Destination." }
}

function Render-Configuration {
  Assert-Name $HostName 'host-name'
  Assert-Name $Environment 'environment'
  Assert-Name $Team 'team'
  Assert-Name $Location 'location'
  Assert-Name $TlsServerName 'tls-server-name'
  Assert-HttpsEndpoint $MetricsEndpoint 'metrics-endpoint'
  Assert-HttpsEndpoint $LogsEndpoint 'logs-endpoint'
  foreach ($pair in @(@($CaFile, 'tls-ca-file'), @($ClientCertificateFile, 'tls-cert-file'), @($ClientKeyFile, 'tls-key-file'))) {
    Assert-File $pair[0] $pair[1]
  }
  foreach ($pack in $EnablePack) { Test-PackPrerequisite $pack }

  $principal = Get-ServicePrincipal
  Protect-CollectorData $principal
  $certDir = Join-Path $DataRoot 'certs'
  Copy-CollectorSecret $CaFile (Join-Path $certDir 'ca.crt')
  Copy-CollectorSecret $ClientCertificateFile (Join-Path $certDir 'client.crt')
  Copy-CollectorSecret $ClientKeyFile (Join-Path $certDir 'client.key')

  $collectors = @($BaseCollectors)
  foreach ($pack in $EnablePack) { $collectors += $PackCollectors[$pack] }
  $collectorValue = (($collectors | Sort-Object -Unique | ForEach-Object { '"{0}"' -f $_ }) -join ', ')
  $securityBlock = if ($IncludeSecurityEventLog) { Get-Content -LiteralPath (Join-Path $TemplateRoot 'security-eventlog.alloy.tmpl') -Raw -Encoding UTF8 } else { '// Security Event Log não foi habilitado para este host.' }
  $config = Get-Content -LiteralPath (Join-Path $TemplateRoot 'config.alloy.tmpl') -Raw -Encoding UTF8
  $config = $config.Replace('__SECURITY_EVENTLOG_BLOCK__', $securityBlock)
  $replacements = @{
    '__WINDOWS_COLLECTORS__' = $collectorValue
    '__HOST_NAME__' = $HostName
    '__ENVIRONMENT__' = $Environment
    '__TEAM__' = $Team
    '__LOCATION__' = $Location
    '__METRICS_ENDPOINT__' = $MetricsEndpoint
    '__LOGS_ENDPOINT__' = $LogsEndpoint
    '__TLS_SERVER_NAME__' = $TlsServerName
  }
  foreach ($token in $replacements.Keys) { $config = $config.Replace($token, $replacements[$token]) }
  if ($config -match '__[A-Z_]+__') { Fail 'template contém placeholder não resolvido.' }

  $configPath = Join-Path $DataRoot 'config.alloy'
  $backupPath = Join-Path $DataRoot 'config.previous.alloy'
  if (Test-Path -LiteralPath $configPath) { Copy-Item -LiteralPath $configPath -Destination $backupPath -Force }
  Set-Content -LiteralPath $configPath -Value $config -Encoding UTF8 -NoNewline
  & icacls.exe $configPath /inheritance:r /grant:r 'SYSTEM:F' "${principal}:R" 'Administrators:F' | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'não foi possível proteger a configuração Alloy.' }
  Log "configuração renderizada em $configPath; Security Event Log: $($IncludeSecurityEventLog.IsPresent)."
}

function Assert-Installer {
  Assert-File $AlloyInstaller 'alloy-installer'
  if ([string]::IsNullOrWhiteSpace($AlloyInstallerSha256)) { Fail 'alloy-installer-sha256 é obrigatório para deploy.' }
  $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $AlloyInstaller).Hash
  if (-not [string]::Equals($actual, $AlloyInstallerSha256, [StringComparison]::OrdinalIgnoreCase)) { Fail 'SHA-256 do instalador Alloy não confere.' }
}

function Save-InstallState {
  $state = [ordered]@{
    schema_version = 1
    configured_at_utc = [DateTime]::UtcNow.ToString('o')
    installer_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $AlloyInstaller).Hash.ToLowerInvariant()
    service_identity = Get-ServicePrincipal
    enabled_packs = @($EnablePack)
    security_eventlog_enabled = [bool]$IncludeSecurityEventLog
  }
  $state | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $DataRoot 'install-state.json') -Encoding UTF8
}

function Install-Collector {
  Assert-Installer
  $configPath = Join-Path $DataRoot 'config.alloy'
  if (-not (Test-Path -LiteralPath $configPath)) { Fail 'configuração ausente; execute primeiro --Phase configure.' }
  $installerArgs = @('/S', "/CONFIG=$configPath", "/D=$InstallRoot")
  Log 'instalando/atualizando Alloy a partir de artefato local com SHA-256 conferido.'
  & $AlloyInstaller @installerArgs
  if ($LASTEXITCODE -ne 0) { Fail "instalador Alloy falhou (exit $LASTEXITCODE)." }
  $binary = Join-Path $InstallRoot 'alloy.exe'
  if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { Fail "alloy.exe não encontrado em $InstallRoot após instalação." }
  & $binary validate $configPath
  if ($LASTEXITCODE -ne 0) { Fail 'alloy validate recusou a configuração renderizada; a configuração anterior foi preservada em config.previous.alloy.' }
  $principal = Get-ServicePrincipal
  & sc.exe config $ServiceName obj= $principal password= '' | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail 'não foi possível definir a identidade do serviço Alloy.' }
  Add-CollectorGroup 'Event Log Readers' $principal
  Add-CollectorGroup 'Performance Monitor Users' $principal
  Save-InstallState
  Restart-Service -Name $ServiceName -Force
  Log 'serviço Alloy atualizado; execute --Phase verify antes de considerar o host integrado.'
}

function Verify-Collector {
  $binary = Join-Path $InstallRoot 'alloy.exe'
  $configPath = Join-Path $DataRoot 'config.alloy'
  Assert-File $binary 'alloy.exe'
  Assert-File $configPath 'config.alloy'
  & $binary validate $configPath
  if ($LASTEXITCODE -ne 0) { Fail 'alloy validate falhou.' }
  $service = Get-Service -Name $ServiceName -ErrorAction Stop
  if ($service.Status -ne 'Running') { Fail "serviço $ServiceName não está em execução ($($service.Status))." }
  $deadline = (Get-Date).AddSeconds(60)
  do {
    try {
      $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:12345/-/ready' -TimeoutSec 3
      if ($response.StatusCode -eq 200) { Log 'Alloy pronto em loopback; valide métricas, logs e alerta no gateway autorizado.'; return }
    } catch { Start-Sleep -Seconds 2 }
  } while ((Get-Date) -lt $deadline)
  Fail 'Alloy não ficou ready em 60s. A porta administrativa permanece apenas em loopback.'
}

function Rollback-Configuration {
  $configPath = Join-Path $DataRoot 'config.alloy'
  $backupPath = Join-Path $DataRoot 'config.previous.alloy'
  Assert-File $backupPath 'config.previous.alloy'
  Copy-Item -LiteralPath $backupPath -Destination $configPath -Force
  Restart-Service -Name $ServiceName -Force
  Log 'configuração anterior restaurada. Rollback binário requer o instalador anterior, verificado e executado pela mudança aprovada.'
}

function Remove-Collector {
  if (-not $ConfirmRemove) { Fail 'remoção exige --ConfirmRemove; os dados e bookmarks são preservados para investigação.' }
  $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
  if ($service) {
    Stop-Service -Name $ServiceName -Force
    & sc.exe delete $ServiceName | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail 'não foi possível remover o serviço Alloy.' }
  }
  Log "serviço removido; dados persistentes não foram apagados: $DataRoot."
}

Assert-Administrator
if (PhaseSelected 'preflight') {
  if (-not (Test-Path -LiteralPath $TemplateRoot -PathType Container)) { Fail 'templates Windows ausentes no pacote.' }
  Log 'preflight concluído: nenhum host, role ou serviço de negócio foi alterado.'
}
if (PhaseSelected 'configure') { Render-Configuration }
if (PhaseSelected 'deploy') { Install-Collector }
if (PhaseSelected 'verify') { Verify-Collector }
if ($Phase -eq 'rollback') { Rollback-Configuration }
if ($Phase -eq 'remove') { Remove-Collector }
