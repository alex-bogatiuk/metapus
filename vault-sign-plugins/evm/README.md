# Metapus EVM Vault secrets plugin

Dedicated Sepolia custody mount for Metapus. It stores a tenant-scoped EVM HD
seed under seal wrap, derives only `m/44'/60'/{account}'/0/{index}` keys, and
signs only policy-authorized EIP-1559 ETH or allowlisted PYUSD transfers.

The v1 chain allowlist contains only Sepolia (`11155111`). This mount must use
an EVM seed that is independent from the TRON plugin seed.

## Security boundary

The plugin does not expose a generic hash-signing endpoint. `tx/sign`
reconstructs a type-2 transaction and checks the authorization JWS, tenant,
chain ID, source/key ownership, nonce, gas and fee cap. Native transfers must
match destination and value exactly. ERC-20 transfers are restricted to the
Sepolia PYUSD contract and exact `transfer(address,uint256)` calldata.

Replacement generations are consecutive and retain one persisted business
intent and nonce. Only fee fields may change. A tenant sweep destination must
first be derived by this mount and then be present in the tenant allowlist.

## Vault paths

All paths are mounted below `evm/tenants/<tenant-id>/`:

- `config/authorization` — operator-only signing keys, independent 64-byte EVM
  seed and the Sepolia chain allowlist;
- `config/sweep-destinations` — operator-only derived consolidation wallets;
- `config/readiness` — non-secret readiness used by preflight and worker;
- `wallets/derive` — idempotent EVM HD derivation;
- `tx/sign` — policy-aware transaction signing.

The worker policy needs only read access to readiness and update access to
derive/sign. It must not read or update authorization, seed or destination
configuration.

## Build and test

```sh
go test ./...
CGO_ENABLED=0 go build -trimpath -o vault-plugin-secrets-evm ./cmd/vault-plugin-secrets-evm
```

Registration and tenant bootstrap are documented in
`docs/operations/vault-evm-sepolia.md`.


## AML signing contract

`GET tenants/<tenant UUID>/config/readiness` advertises binary support using
`aml_signing_schema=aml_signing.v1`, `aml_intent_schema=money_movement_intent.v2`
and `aml_source_line_basis=CryptoAMLWithdrawalLine`. The contract binds explicit
risk acceptance and the complete immutable source plan; each source line is
consumed once across approval changes and allows attempt/generation 1 only.

Custody support alone does not satisfy AML readiness. Workers check this
contract before new signing or broadcast when the network has enabled AML
scopes. Deploy compatible plugins before activating those scopes, and run the
[read-only AML configuration preflight](../../docs/operations/crypto-aml-preflight.md).
No signing or enrollment is performed by that check.
