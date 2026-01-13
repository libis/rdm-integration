# Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

STAGE ?= dev
BUILD_BASE_HREF ?= /integration/

include env.$(STAGE)
include .env
export

.SILENT:
SHELL = /bin/bash

# Staticcheck binary location: prefer GOBIN, fall back to GOPATH/bin
STATICCHECK_BIN := $(or $(shell go env GOBIN 2>/dev/null),$(shell go env GOPATH)/bin)/staticcheck

# Set USER_ID and GROUP_ID to current user's uid and gid if not set
USER_ID ?= $(shell id -u)
GROUP_ID ?= $(shell id -g)

DEV_SENTINEL := .dev_up_active
DEV_COMPOSE := docker compose -f docker-compose.yml -f docker-compose.yml.dev
DATAVERSE_EXPLODED := docker-volumes/dataverse/applications/dataverse.war
DATAVERSE_CLASSES_DST := $(DATAVERSE_EXPLODED)/WEB-INF/classes
DATAVERSE_CLASSES_SRC := ../dataverse/target/classes
DATAVERSE_WEBAPP_SRC := ../dataverse/src/main/webapp

build: fmt ## Build Docker image
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
	@echo "Downloading latest Solr configuration files..."
	@wget -q --show-progress https://raw.githubusercontent.com/IQSS/dataverse/refs/heads/develop/conf/solr/schema.xml -O conf/solr/schema.xml
	@wget -q --show-progress https://raw.githubusercontent.com/IQSS/dataverse/refs/heads/develop/conf/solr/solrconfig.xml -O conf/solr/solrconfig.xml
	@wget -q --show-progress https://raw.githubusercontent.com/IQSS/dataverse/refs/heads/develop/conf/solr/update-fields.sh -O dataverse/update-fields.sh
	@chmod +x dataverse/update-fields.sh

init: ## initialize docker volumes before running the server locally
	@echo -n "Initializing Docker volumes..."
	docker compose -f docker-compose.yml down || true
	rm -rf docker-volumes
	mkdir -p docker-volumes/{cache/data,dataverse/{data/{filestore,uploads,exporters},secrets/api,conf},integration/{aws,conf,data,go-mod-cache,go-build-cache},solr/{conf,data},postgresql/data,keycloak/conf,localstack/{conf,data},minio/data/mybucket}
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
	@echo -n "Waiting for Dataverse initialization "
	@while [ ! -f docker-volumes/dataverse/data/initialized ]; do \
		[[ $$? -gt 0 ]] && echo -n 'x' || echo -n '.'; sleep 1; done && true
	@echo	' OK.'
	@make install-cdi-exporter
	@echo -n "Shutting down initialized Dataverse..."
	docker compose -f docker-compose.yml down

clean: ## delete docker volumes
	rm -rf docker-volumes
	rm -f $(DEV_SENTINEL)

up: ## Run the server locally
	rm -f $(DEV_SENTINEL)
	if [ ! -f docker-volumes/dataverse/data/initialized ]; then \
		$(MAKE) init; \
	fi
	docker compose -f docker-compose.yml up -d --build
	@echo -n "Waiting for Dataverse ready "
	@while [ "$$(curl -sk -m 1 -I http://localhost:8080/api/info/version | head -n 1 | cut -d$$' ' -f2)" != "200" ]; do \
		[[ $$? -gt 0 ]] && echo -n 'x' || echo -n '.'; sleep 1; done && true
	@echo	' OK.'

install-cdi-exporter: ## Install CDI exporter
	@if [ ! -f docker-volumes/dataverse/data/initialized ]; then \
		echo "Error: Dataverse not initialized. Run 'make up' first." >&2; \
		exit 1; \
	fi
	@echo "Installing CDI support for Dataverse..."
	@SERVER_URL="http://localhost:8080"; \
	EXPORTERS_DIR="docker-volumes/dataverse/data/exporters"; \
	echo ""; \
	echo "1. Installing CDI Exporter..."; \
	if [ ! -d "$$EXPORTERS_DIR" ]; then \
		echo "   Creating exporters directory..."; \
		docker exec dataverse mkdir -p /dv/exporters; \
		docker exec dataverse bash -c "curl -X PUT -d '/dv/exporters' http://localhost:8080/api/admin/settings/:dataverse-spi-exporters-directory?unblock-key=$$(cat docker-volumes/dataverse/secrets/api/key)" >/dev/null 2>&1; \
	fi; \
	if [ ! -f "$$EXPORTERS_DIR/exporter-transformer-1.0.10-jar-with-dependencies.jar" ]; then \
		echo "   Downloading exporter-transformer JAR..."; \
		docker exec dataverse bash -c "cd /dv/exporters && wget -q https://repo1.maven.org/maven2/io/gdcc/export/exporter-transformer/1.0.10/exporter-transformer-1.0.10-jar-with-dependencies.jar"; \
	fi; \
	echo "   Installing CDI exporter configuration..."; \
	docker exec dataverse mkdir -p /dv/exporters/cdi-exporter; \
	docker exec dataverse bash -c "cd /dv/exporters/cdi-exporter && wget -q -O config.json https://raw.githubusercontent.com/gdcc/exporter-transformer/main/examples/cdi-exporter/config.json"; \
	docker exec dataverse bash -c "cd /dv/exporters/cdi-exporter && wget -q -O transformer.py https://raw.githubusercontent.com/gdcc/exporter-transformer/main/examples/cdi-exporter/transformer.py"; \
	echo "   ✓ CDI Exporter installed"; \
	echo "";

frd-dataverse:
	@if [ ! -f $(DEV_SENTINEL) ]; then \
		echo "Error: frd-dataverse requires the dev stack (run 'make dev_up' first)." >&2; \
		exit 1; \
	fi
	@echo -n "frd-dataverse: compiling Dataverse sources "
	cd ../dataverse && mvn -T 1C -DskipTests=true -DskipUnitTests=true -DskipIntegrationTests=true compile >/dev/null
	@echo 'OK.'
	@if [ ! -d "$(DATAVERSE_CLASSES_SRC)" ]; then \
		echo "ERROR: $(DATAVERSE_CLASSES_SRC) missing after compile." >&2; \
		exit 1; \
	fi
	rsync -a --delete --exclude 'META-INF/persistence.xml' "$(DATAVERSE_CLASSES_SRC)/" "$(DATAVERSE_CLASSES_DST)/"
	@if [ -d "$(DATAVERSE_WEBAPP_SRC)" ]; then \
		rsync -a --delete \
			--exclude 'WEB-INF/classes' --exclude 'WEB-INF/classes/**' \
			--exclude 'WEB-INF/lib' --exclude 'WEB-INF/lib/**' \
		"$(DATAVERSE_WEBAPP_SRC)/" "$(DATAVERSE_EXPLODED)/"; \
	fi
	@echo -n "Deploying updated dataverse.war ... "
	docker exec dataverse bash -lc '\
		printf "AS_ADMIN_PASSWORD=%s\n" admin > /tmp/pwdfile; \
		output=$$(asadmin --user admin --passwordfile /tmp/pwdfile deploy --force=true --upload=false /opt/payara/appserver/glassfish/domains/domain1/applications/dataverse.war 2>&1); \
		status=$$?; \
		rm /tmp/pwdfile; \
		printf "%s\n" "$$output" | awk '\''!/PER0100[03]/ && !/Command deploy completed with warnings./ {print}'\''; \
		exit $$status'

frd-integration:
	@if [ ! -f $(DEV_SENTINEL) ]; then \
		echo "Error: frd-integration requires the dev stack (run 'make dev_up' first)." >&2; \
		exit 1; \
	fi
	$(DEV_COMPOSE) restart integration
	@echo -n "Waiting for dev rdm-integration ready "
	@while [ "$$(curl -sk -m 1 -o /dev/null -w '%{http_code}' http://localhost:7788/)" != "200" ]; do \
		[[ $$? -gt 0 ]] && echo -n 'x' || echo -n '.'; sleep 1; done && true
	@echo '\t OK.'

dev_up: ## Run the development frontend version locally
	if [ ! -f docker-volumes/dataverse/data/initialized ]; then \
		$(MAKE) init; \
	fi
	docker compose -f docker-compose.yml -f docker-compose.yml.dev rm -sf dataverse
	rm -rf docker-volumes/dataverse/applications/*
	echo "Building dataverse..."
	cd ../dataverse && mvn -T 1C -DskipTests=true -DskipUnitTests=true -DskipIntegrationTests=true clean package
	mkdir -p $(DATAVERSE_EXPLODED)
	unzip -oq ../dataverse/target/dataverse-$(DATAVERSE_VERSION).war -d $(DATAVERSE_EXPLODED)
	rsync -a "../dataverse/target/dataverse/" $(DATAVERSE_EXPLODED)/
	# Disable JPA DDL generation in dev to avoid sequence conflicts during redeploys.
	sed -i 's/\(eclipselink.ddl-generation" value="\)create-tables/\1none/' \
		$(DATAVERSE_CLASSES_DST)/META-INF/persistence.xml
	docker compose -f docker-compose.yml -f docker-compose.yml.dev up -d --build
	@echo -n "Waiting for server ready "
	@while [ "$$(curl -sk -m 1 -o /dev/null -w '%{http_code}' http://localhost:8080/)" != "200" ]; do \
		[[ $$? -gt 0 ]] && echo -n 'x' || echo -n '.'; sleep 1; done && true
	@echo	' OK.'
	@echo -n "Deploying updated dataverse.war ... "
	docker exec dataverse bash -lc '\
		printf "AS_ADMIN_PASSWORD=%s\n" admin > /tmp/pwdfile; \
		output=$$(asadmin --user admin --passwordfile /tmp/pwdfile deploy --upload=false /opt/payara/appserver/glassfish/domains/domain1/applications/dataverse.war 2>&1); \
		status=$$?; \
		rm /tmp/pwdfile; \
		printf "%s\n" "$$output" | awk '\''!/PER0100[03]/ && !/Command deploy completed with warnings./ {print}'\''; \
		exit $$status'
	@touch $(DEV_SENTINEL)

dev_build: fmt ## Build Docker image using local frontend (like dev_up but only builds; respects STAGE)
	@echo -n "Building integration frontend... "
	cd ../rdm-integration-frontend && git archive --format=tar.gz -o ../rdm-integration/$(FRONTEND_VERSION).tar.gz \
		--prefix=rdm-integration-frontend-$(FRONTEND_VERSION)/ \
		$$(if [[ $$(git stash create) ]]; then git stash create; else git rev-parse HEAD; fi)
	@echo -n "Building Docker image (STAGE=$(STAGE)) using local frontend... "
	docker build \
		--build-arg USER_ID=$(USER_ID) --build-arg GROUP_ID=$(GROUP_ID) \
		--build-arg OAUTH2_POXY_VERSION=$(OAUTH2_POXY_VERSION) --build-arg NODE_VERSION=$(NODE_VERSION) \
		--build-arg FRONTEND_VERSION=$(FRONTEND_VERSION) --build-arg FRONTEND_TAR_GZ=$(FRONTEND_VERSION).tar.gz \
		--build-arg NODE_ENV=$(NODE_ENV) \
		--build-arg BASE_HREF=$(BUILD_BASE_HREF) --build-arg CUSTOMIZATIONS=$(CUSTOMIZATIONS) \
		--tag "$(IMAGE_TAG)" --file image/Dockerfile .
	@echo -n "Cleaning up local frontend archive... "
	@rm -f $(FRONTEND_VERSION).tar.gz

down: ## Stop the server locally
	rm -f $(DEV_SENTINEL)
	docker compose -f docker-compose.yml down

fmt: ## Format the go code
	cd image && go fmt ./app/...

staticcheck: ## staticcheck the go code
	cd image && $(STATICCHECK_BIN) ./app/...

upgrade_dependencies: ## upgrade all go dependencies
	cd image && go get -u ./app/...
	cd image && go mod tidy

test: ## Run tests (Python + Go)
	cd image && ./run_tests.sh

test-go: ## Run Go tests only
	cd image && go test -v ./app/...

test-python: ## Run Python tests only
	cd image && python3 -m venv venv && source venv/bin/activate && pip install -q -r requirements.txt && python test_csv_to_cdi.py

benchmark: ## Run benchmarks
	cd image && go test -bench=. -benchmem ./app/...

coverage: ## Run tests with coverage
	cd image && go test -coverprofile=coverage.out ./app/...
	cd image && go tool cover -html=coverage.out -o coverage.html
	@echo -n "Coverage report generated: image/coverage.html "
