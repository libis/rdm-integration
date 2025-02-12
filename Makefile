# Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

STAGE ?= dev
BUILD_BASE_HREF ?= /integration/

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
		--build-arg BASE_HREF=$(BUILD_BASE_HREF) --build-arg CUSTOMIZATIONS=$(CUSTOMIZATIONS) \
		--tag "$(IMAGE_TAG)" --file image/Dockerfile .

push: ## Push Docker image (only in prod stage)
	if [ "$(STAGE)" = "prod" ]; then \
		echo "Pushing Docker image to repository ..."; \
		docker push $(IMAGE_TAG); \
	else \
		echo "Not in production stage. Pushing not allowed."; \
	fi

solr_latest_config: ## update solr config files with latest version from github
	wget https://raw.githubusercontent.com/IQSS/dataverse/refs/heads/develop/conf/solr/schema.xml -O conf/solr/schema.xml
	wget https://raw.githubusercontent.com/IQSS/dataverse/refs/heads/develop/conf/solr/solrconfig.xml -O conf/solr/solrconfig.xml
	wget https://raw.githubusercontent.com/IQSS/dataverse/refs/heads/develop/conf/solr/update-fields.sh -O dataverse/update-fields.sh

init: ## initialize docker volumes before running the server locally
	docker compose -f docker-compose.yml down || true
	rm -rf docker-volumes
	mkdir -p docker-volumes/cache/data
	mkdir -p docker-volumes/dataverse/data/docroot
	mkdir -p docker-volumes/dataverse/data/temp
	mkdir -p docker-volumes/dataverse/data/uploads
	mkdir -p docker-volumes/dataverse/data/exporters
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
	mkdir -p docker-volumes/minio/data/mybucket
	echo -n 'secret-admin-password' > docker-volumes/dataverse/secrets/password
	echo -n 'secret-unblock-key' > docker-volumes/dataverse/secrets/api/key
	echo AWS_ACCESS_KEY_ID=4cc355_k3y > docker-volumes/integration/aws/aws.env
	echo -n AWS_SECRET_ACCESS_KEY=s3cr3t_4cc355_k3y >> docker-volumes/integration/aws/aws.env
	cp -R conf/dataverse/* docker-volumes/dataverse/conf
	cp -R conf/customizations docker-volumes/integration/conf/customizations
	cp -R conf/solr/* docker-volumes/solr/conf
	cp conf/backend_config.json docker-volumes/integration/conf/backend_config.json
	cp conf/frontend_config.json docker-volumes/integration/conf/frontend_config.json
	cp conf/example_oauth_secrets.json docker-volumes/integration/data/example_oauth_secrets.json
	cp conf/oauth2-proxy.cfg docker-volumes/integration/conf/oauth2-proxy.cfg
	cp conf/localstack/buckets.sh docker-volumes/localstack/conf/buckets.sh
	cp conf/keycloak/test-realm.json docker-volumes/keycloak/conf/test-realm.json
	docker compose -f docker-compose.yml up -d --build
	@echo -n "Waiting for Dataverse initialized "
	@while [ ! -f docker-volumes/dataverse/data/initialized ]; do \
		[[ $$? -gt 0 ]] && echo -n 'x' || echo -n '.'; sleep 1; done && true
	@echo	' OK.'
	docker compose -f docker-compose.yml down

clean: ## delete docker volumes
	rm -rf docker-volumes

up: ## Run the server locally
	if [ ! -f docker-volumes/dataverse/data/initialized ]; then \
    	$(MAKE) init; \
	fi
	docker compose -f docker-compose.yml up -d --build
	@echo -n "Waiting for Dataverse ready "
	@while [ "$$(curl -sk -m 1 -I http://localhost:8080/api/info/version | head -n 1 | cut -d$$' ' -f2)" != "200" ]; do \
		[[ $$? -gt 0 ]] && echo -n 'x' || echo -n '.'; sleep 1; done && true
	@echo	' OK.'

dev_up: ## Run the development frontend version locally
	echo "Building integration frontend..."
	cd ../rdm-integration-frontend && git archive --format=tar.gz -o ../rdm-integration/$(FRONTEND_VERSION).tar.gz --prefix=rdm-integration-frontend-$(FRONTEND_VERSION)/ HEAD
	echo "Building dataverse..."
	cd ../dataverse && mvn -DskipTests=true clean package
	cp ../dataverse/target/dataverse-$(DATAVERSE_VERSION).war dataverse-$(DATAVERSE_VERSION).war
	$(MAKE) up FRONTEND_TAR_GZ=$(FRONTEND_VERSION).tar.gz DATAVERSE_WAR_URL=dataverse-$(DATAVERSE_VERSION).war
	rm $(FRONTEND_VERSION).tar.gz
	rm dataverse-$(DATAVERSE_VERSION).war

down: ## Stop the server locally
	docker compose -f docker-compose.yml down

fmt: ## Format the go code
	cd image && go fmt ./app/...

staticcheck: ## staticcheck the go code
	cd image && $(shell go env GOPATH)/bin/staticcheck ./app/...

upgrade_dependencies: ## upgrade all go dependencies
	cd image && go get -u ./app/...
	cd image && go mod tidy
