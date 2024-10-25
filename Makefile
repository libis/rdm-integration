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
	mkdir -p docker-volumes/cache/data
	mkdir -p docker-volumes/dataverse/data/docroot
	mkdir -p docker-volumes/dataverse/data/temp
	mkdir -p docker-volumes/dataverse/data/uploads
	mkdir -p docker-volumes/dataverse/secrets/api
	mkdir -p docker-volumes/dataverse/conf
	mkdir -p docker-volumes/integration/aws
	mkdir -p docker-volumes/integration/conf
	mkdir -p docker-volumes/integration/data
	mkdir -p docker-volumes/solr/conf
	mkdir -p docker-volumes/solr/data
	mkdir -p docker-volumes/postgresql/data
	mkdir -p docker-volumes/keycloak/conf
	mkdir -p docker-volumes/localstack/conf
	mkdir -p docker-volumes/localstack/data
	echo -n 'secret-admin-password' > docker-volumes/dataverse/secrets/password
	echo -n 'secret-unblock-key' > docker-volumes/dataverse/secrets/api/key
	echo AWS_ACCESS_KEY_ID=default > docker-volumes/integration/aws/aws.env
	echo -n AWS_SECRET_ACCESS_KEY=default >> docker-volumes/integration/aws/aws.env
	cp -R conf/dataverse/* docker-volumes/dataverse/conf
	cp -R conf/customizations docker-volumes/integration/conf/customizations
	cp conf/backend_config.json docker-volumes/integration/conf/backend_config.json
	cp conf/frontend_config.json docker-volumes/integration/conf/frontend_config.json
	cp conf/example_oauth_secrets.json docker-volumes/integration/data/example_oauth_secrets.json
	cp conf/oauth2-proxy.cfg docker-volumes/integration/conf/oauth2-proxy.cfg
	cp conf/solr/schema.xml docker-volumes/solr/conf/schema.xml
	cp conf/solr/solrconfig.xml docker-volumes/solr/conf/solrconfig.xml
	cp conf/localstack/buckets.sh docker-volumes/localstack/conf/buckets.sh
	cp conf/keycloak/test-realm.json docker-volumes/keycloak/conf/test-realm.json

clean: ## delete docker volumes
	rm -rf docker-volumes

up: init ## Run the server locally
	docker compose -f docker-compose.yml up -d --build

down: ## Stop the server locally
	docker compose -f docker-compose.yml down

fmt: ## Format the go code
	cd image && go fmt ./app/...

staticcheck: ## staticcheck the go code
	cd image && ~/go/bin/staticcheck ./app/...

upgrade_dependencies: ## upgrade all go dependencies
	cd image && go get -u ./app/...
	cd image && go mod tidy
