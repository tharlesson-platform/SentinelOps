# SPEC-008 — Rede, SNMP e FortiGate

**Fase:** E3 · **Estado:** IN_PROGRESS · **Responsável:** Redes/SRE

## Problema, escopo e contratos

Descoberta de rede é limitada a allowlists aprovadas. SNMPv3 authPriv, ICMP,
TCP, DNS, HTTP, syslog e traps complementam polling. FortiGate usa API
read-only ou SNMP homologado; credenciais vêm de vault e não são exibidas.

## Dados, UX, autorização e operação

Interfaces, banda, erros, discard, link, CPU, memória, temperatura, uptime,
VPN, SD-WAN, HA e certificados mantêm dispositivo/site/owner estáveis.
Counter reset, wrap ou timeout viram estado inconclusivo com provenance, não
tráfego impossível ou healthy.

## Critérios e evidência

- **AC-0801:** Dado reboot ou counter wrap, quando taxa é calculada, então não
  há banda negativa ou impossível.
- **AC-0802:** Dada interface down ou VPN degradada de homologação, quando o
  sinal chega, então alerta tem severidade, owner e janela corretos.
- **AC-0803:** Dado timeout/rotação de credencial, quando coleta falha, então
  erro é observável sem escrita de configuração no equipamento.
- **AC-0804:** Dado trap isolado, quando não há polling corroborante, então
  disponibilidade não é declarada apenas pelo trap.

O primeiro corte implementa `snmp_exporter` SNMPv3 authPriv sem porta pública,
allowlist JSON de IPs exatos renderizada para Alloy, mTLS ao gateway e testes
negativos contra hostname/CIDR. O perfil Syslog UDP/TCP é opcional, exige bind
em IP literal não-loopback e egress Loki mTLS, e descarta linhas que aparentem
conter segredo. Não há descoberta de rede, traps ou validação em equipamento.
Teste em alvo requer equipamento de laboratório e conta read-only. Rollout por
site e taxa limitada; rollback remove somente o collector/credencial.
