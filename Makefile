# Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

STAGE ?= dev
BASE_HREF ?= /integration/

include env.$(STAGE)
include .env
export

.SILENT:
SHELL = /bin/bash

# Set USER_ID and GROUP_ID to current user's uid and gid if not set
USER_ID ?= $(shell id -u)
GROUP_ID ?= $(shell id -g)

build: fmt staticcheck ## Build Docker image
	docker build \
		--build-arg USER_ID=$(USER_ID) --build-arg GROUP_ID=$(GROUP_ID) \
		--build-arg OAUTH2_POXY_VERSION=$(OAUTH2_POXY_VERSION) --build-arg NODE_VERSION=$(NODE_VERSION) \
		--build-arg FRONTEND_VERSION=$(FRONTEND_VERSION) --build-arg NODE_ENV=$(NODE_ENV) \
		--build-arg BASE_HREF=$(BASE_HREF) --build-arg CUSTOMIZATIONS=$(CUSTOMIZATIONS) \
		--tag "$(IMAGE_TAG)" ./image

push: ## Push Docker image (only in prod stage)
	if [ "$(STAGE)" = "prod" ]; then \
		echo "Pushing Docker image to repository ..."; \
		docker push $(IMAGE_TAG); \
	else \
		echo "Not in production stage. Pushing not allowed."; \
	fi

init: ## initialize docker volumes before running the server locally
	rm ${SMAPLE_DATA_VERSION}.tar.gz || true
	wget https://github.com/libis/rdm-integration-sample-data/archive/refs/tags/${SMAPLE_DATA_VERSION}.tar.gz
	tar -xzf ${SMAPLE_DATA_VERSION}.tar.gz rdm-integration-sample-data-${SMAPLE_DATA_VERSION}/docker-volumes --strip-components=1
	find ./docker-volumes -type f -name '.gitignore' -exec rm {} +

run: ## Run the server locally
	docker compose -f docker-compose.yml up -d --build

fmt: ## Format the go code
	cd image && go fmt ./app/...

staticcheck: ## staticcheck the go code
	cd image && ~/go/bin/staticcheck ./app/...

upgrade_dependencies: ## upgrade all go dependencies
	cd image && go get -u ./app/...
	cd image && go mod tidy
