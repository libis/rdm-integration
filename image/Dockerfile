# Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License
ARG OAUTH2_POXY_VERSION=v7.6.0
ARG NODE_VERSION=22-alpine

FROM node:${NODE_VERSION} AS frontend-builder

# Download and extract frontend
WORKDIR /app
ARG FRONTEND_VERSION=1.0.0
ADD https://github.com/libis/rdm-integration-frontend/archive/refs/tags/${FRONTEND_VERSION}.tar.gz /app/
RUN tar -xzf ${FRONTEND_VERSION}.tar.gz

# Install packages
WORKDIR /app/rdm-integration-frontend-${FRONTEND_VERSION}
RUN --mount=type=cache,target=/app/rdm-integration-frontend-${FRONTEND_VERSION}/node_modules yarn install --network-timeout 300000

# Build
ARG NODE_ENV=production
ARG BASE_HREF=/
RUN --mount=type=cache,target=/app/rdm-integration-frontend-${FRONTEND_VERSION}/node_modules yarn ng build --configuration=${NODE_ENV} --base-href=${BASE_HREF}

# Customize
ARG CUSTOMIZATIONS=./conf/kul_customizations
COPY ${CUSTOMIZATIONS} /app/rdm-integration-frontend-${FRONTEND_VERSION}/dist/datasync/

FROM golang:alpine AS backend-builder

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY ./image/go.mod ./image/go.sum ./
RUN go mod download && go mod verify

COPY ./image .
ARG FRONTEND_VERSION=1.0.0
COPY --from=frontend-builder /app/rdm-integration-frontend-${FRONTEND_VERSION}/dist ./app/frontend/dist
RUN go build -ldflags "-s -w" -v -o /usr/local/bin/app ./app
RUN go build -ldflags "-s -w" -v -o /usr/local/bin/workers ./app/workers

FROM quay.io/oauth2-proxy/oauth2-proxy:${OAUTH2_POXY_VERSION}-alpine

RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
COPY ./image/USERTrust_RSA_Certification_Authority.pem /usr/local/share/ca-certificates/USERTrust_RSA_Certification_Authority.pem
RUN update-ca-certificates

ARG USER_ID
ARG GROUP_ID
RUN addgroup --gid ${GROUP_ID} app \
 && adduser --disabled-password --uid ${USER_ID} --ingroup app --gecos '' app

COPY --from=backend-builder /usr/local/bin /usr/local/bin

USER app

ENTRYPOINT ["app"]
