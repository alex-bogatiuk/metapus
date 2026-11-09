FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS plugin-builder

WORKDIR /src
COPY vault-sign-plugins/tron/go.mod vault-sign-plugins/tron/go.sum ./
RUN go mod download
COPY vault-sign-plugins/tron/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vault-plugin-secrets-tron ./cmd/vault-plugin-secrets-tron

FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS evm-plugin-builder

WORKDIR /src
COPY vault-sign-plugins/evm/go.mod vault-sign-plugins/evm/go.sum ./
RUN go mod download
COPY vault-sign-plugins/evm/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vault-plugin-secrets-evm ./cmd/vault-plugin-secrets-evm

FROM hashicorp/vault:2.0.3@sha256:a296a888b118615dc01d5f1a6846e6d4a7277946caaed5b447008fff5fe06b54
USER root
COPY --from=plugin-builder --chown=vault:vault /out/vault-plugin-secrets-tron /vault/plugins/vault-plugin-secrets-tron
COPY --from=evm-plugin-builder --chown=vault:vault /out/vault-plugin-secrets-evm /vault/plugins/vault-plugin-secrets-evm
RUN mkdir -p /vault/data \
    && chown vault:vault /vault/data \
    && chmod 0700 /vault/data \
    && chmod 0550 /vault/plugins/vault-plugin-secrets-tron \
    && chmod 0550 /vault/plugins/vault-plugin-secrets-evm
USER vault
