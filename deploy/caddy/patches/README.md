# Migração JOSE do Caddy

O binário Caddy 2.11.4 incluía go-jose/v3.0.5 por meio de Smallstep.
O advisory [GHSA-mgcc-cww5-fp2q](https://github.com/go-jose/go-jose/security/advisories/GHSA-mgcc-cww5-fp2q)
não oferece release corrigido da linha v3. A migração usa a implementação
real go-jose/v4.1.6, preservando PKI, provisioners e TLS/mTLS do Caddy.

## Fontes reproduzíveis

| Fonte | Base imutável | Patch SHA-256 |
|---|---|---|
| smallstep/crypto v0.91.1 | `516f3a2010a5c63c4e71357a3f813e0b9e84d090` | `651f6b95514aee2b3e6b44dd44658eaf8d31b98355fdd0de501ccf3c938ea75c` |
| smallstep/certificates v0.30.2 | `6e8ec61405239cf3f37b2bbf260a587b7d2e4e31` | `77735dc48e203427c4523e3fd28b3539431c63c4c72f5090f65adf30fd9b73ef` |

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
