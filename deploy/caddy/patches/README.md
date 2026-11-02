# Migração JOSE do Caddy

O binário Caddy 2.11.4 incluía go-jose/v3.0.5 por meio de Smallstep.
O advisory [GHSA-mgcc-cww5-fp2q](https://github.com/go-jose/go-jose/security/advisories/GHSA-mgcc-cww5-fp2q)
não oferece release corrigido da linha v3. A migração usa a implementação
real go-jose/v4.1.6, preservando PKI, provisioners e TLS/mTLS do Caddy.

## Fontes reproduzíveis

| Fonte | Base imutável | Patch SHA-256 |
|---|---|---|
| smallstep/crypto v0.91.1 | `516f3a2010a5c63c4e71357a3f813e0b9e84d090` | `16516c075a50efb8e10adef4fa6f44cca2c89d3f16f4eaff353d4f2580a39595` |
| smallstep/certificates v0.30.2 | `6e8ec61405239cf3f37b2bbf260a587b7d2e4e31` | `b667803644f8d9ab31462078166277da0f2a6ab52834b83133a61bf9f69aecbd` |

A migração inicial de crypto deriva da
[PR upstream #1157](https://github.com/smallstep/crypto/pull/1157), head
`00c45432f48558f92a6e7da6d885d94e90db4d7a`. O build usa a base estável
acima mais o patch versionado, sem depender da disponibilidade futura da
branch dessa PR. Os forks não são releases upstream: replacements locais
permanecem explícitos no módulo principal e no inventário Go do binário.
O Dockerfile verifica commits e hashes antes de aplicar os patches.

## Compatibilidade e verificações

- `CompactSerialize` do JWT Builder delega à serialização compacta real de
  v4, preservando claims encadeados e propagação de erros.
- Os quatro `Expected.Audience` de certificates tornam-se `AnyAudience`.
  Todos esperam um único valor; issuer, tempo, leeway e verificações de tenant
  continuam obrigatórios.
- O adaptador X25519 omite somente o metadado JWK opcional da API opaca
  oficial. XEdDSA, chave e assinatura permanecem reais. Solicitações de JWK
  embutido falham explicitamente; signers genéricos continuam submetidos à
  validação normal de chave pública. Não há conversão de curva.
- Testes de cifragem usam `testing/cryptotest` para determinismo. A DI privada
  testa propagação de falhas do wrapper; funções públicas sempre recebem
  factories reais. Falhas do gerador interno JOSE são verificadas pela suíte
  original v4.1.6, sem alterar essa biblioteca.
- Um fixture Ed25519 upstream tinha `x` inconsistente com `d`. O original
  permanece como negativo. O par positivo usa a mesma seed privada, chave
  pública derivada e thumbprint RFC coerentes. São vetores públicos de teste.

Os patches contêm fontes e manifestos; não duplicam `go.sum` gerado nem o
JWE serializado do fixture. O Go obtém e verifica checksums usando os
manifestos fixados e sumdb. O comando de build `internal/compatfixtures`
cifra o JWK público de teste corrigido com JOSE real e a senha pública
upstream, verificando seu round trip antes das suítes. O fixture inválido
original continua byte a byte como teste negativo. Nenhuma regra ou
allowlist do scan de segredos é alterada.

O estágio de build executa as suítes JOSE, provisioners, stepcas e ACME,
incluindo a suíte upstream go-jose/v4. O compilador roda na plataforma
nativa; somente o build final recebe TARGETOS/TARGETARCH, com CGO desativado.

O módulo principal também fixa `github.com/klauspost/compress` em v1.18.7,
release de segurança que corrige a leitura fora de limites de `s2.NewDict`
(CVE-2026-63209). A verificação considera as dependências realmente presentes
no binário, incluindo os replacements locais.

Antes do aceite: conferir Go 1.26.10 no binário, v4.1.6 presente e v3 ausente;
validar os dois Caddyfiles, mTLS positivo/negativo, isolamento por tenant e
HTTP/gRPC; executar os scans da imagem exata e todos os gates do novo SHA.
Testes de módulo ou revisão de fonte não substituem esse aceite integrado.

Esta mudança não altera tags de produção nem executa rollout. Reverter para
o binário v3 reintroduz o finding conhecido; rollback precisa considerar
essa exposição e manter as validações de segurança.
