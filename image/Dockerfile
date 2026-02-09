# Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License
ARG OAUTH2_POXY_VERSION=v7.14.2
ARG NODE_VERSION=24-alpine

FROM node:${NODE_VERSION} AS frontend-builder

# Download and extract frontend
WORKDIR /app
ARG FRONTEND_VERSION=2.0.0
ARG FRONTEND_TAR_GZ=https://github.com/libis/rdm-integration-frontend/archive/refs/tags/${FRONTEND_VERSION}.tar.gz
ADD ${FRONTEND_TAR_GZ} /app/
RUN tar -xzf ${FRONTEND_VERSION}.tar.gz || true

# Install packages with better caching
WORKDIR /app/rdm-integration-frontend-${FRONTEND_VERSION}
RUN --mount=type=cache,target=/root/.npm \
    npm install --prefer-offline --no-audit --progress=false

# Build
ARG NODE_ENV=production
ARG BASE_HREF=/
RUN --mount=type=cache,target=/root/.npm \
    npm run build -- --configuration=${NODE_ENV} --base-href=${BASE_HREF}

# Customize
ARG CUSTOMIZATIONS=./conf/kul_customizations
COPY ${CUSTOMIZATIONS} /app/rdm-integration-frontend-${FRONTEND_VERSION}/dist/datasync/browser/

FROM golang:alpine AS backend-builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY ./image/go.mod ./image/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY ./image .
ARG FRONTEND_VERSION=2.0.0
COPY --from=frontend-builder /app/rdm-integration-frontend-${FRONTEND_VERSION}/dist/datasync/browser ./app/frontend/dist/datasync

# Build with optimizations
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -extldflags '-static'" -a -installsuffix cgo -v -o /usr/local/bin/app ./app
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -extldflags '-static'" -a -installsuffix cgo -v -o /usr/local/bin/workers ./app/workers

FROM quay.io/oauth2-proxy/oauth2-proxy:${OAUTH2_POXY_VERSION}-alpine

RUN apk update && apk add --no-cache ca-certificates bash python3 py3-pip gzip fuse fuse3 s3fs-fuse && rm -rf /var/cache/apk/*
COPY ./image/USERTrust_RSA_Certification_Authority.pem /usr/local/share/ca-certificates/USERTrust_RSA_Certification_Authority.pem
RUN update-ca-certificates

RUN python3 -m pip install --no-cache-dir --break-system-packages --upgrade pip && \
    python3 -m pip install --no-cache-dir --break-system-packages rdflib==7.5.0 chardet==5.2.0 datasketch==1.9.0 python-dateutil==2.9.0.post0

ARG USER_ID
ARG GROUP_ID
# Create group if it doesn't exist (handles macOS GID 20 = staff group)
RUN if ! getent group ${GROUP_ID} >/dev/null; then \
        addgroup --gid ${GROUP_ID} app; \
    else \
        GROUP_NAME=$(getent group ${GROUP_ID} | cut -d: -f1); \
        if [ "$GROUP_NAME" != "app" ]; then \
            addgroup app && GROUP_ID=$(getent group app | cut -d: -f3); \
        fi; \
    fi \
 && adduser --disabled-password --uid ${USER_ID} --ingroup $(getent group ${GROUP_ID} | cut -d: -f1) --gecos '' app

COPY --from=backend-builder /usr/local/bin /usr/local/bin

COPY ./image/cdi_generator_jsonld.py /usr/local/bin/cdi_generator_jsonld.py
COPY ./xconvert /usr/local/bin/xconvert
RUN chmod +x /usr/local/bin/cdi_generator_jsonld.py /usr/local/bin/xconvert

USER app

ENTRYPOINT ["app"]
