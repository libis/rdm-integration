STAGE ?= dev

include env.$(STAGE)
include .env
export

.SILENT:
SHELL = /bin/bash

# Set USER_ID and GROUP_ID to current user's uid and gid if not set
USER_ID ?= $(shell id -u)
GROUP_ID ?= $(shell id -g)

build: fmt ## Build Docker image
	echo "Building frontend ..."
	cd ../rdm-integration-frontend && rm -rf ./dist && ng build --configuration="production" --base-href /integration/
	echo "Building Docker image ..."
	rm -rf image/dist
	cp -r ../rdm-integration-frontend/dist image/dist
	docker build \
		--build-arg USER_ID=$(USER_ID) --build-arg GROUP_ID=$(GROUP_ID) \
		--tag "$(IMAGE_TAG)" ./image

redeploy: ## Deploy Docker image
	cd ../rdm-deployment && make stop-integration && make up

push: ## Push Docker image (only in prod stage)
	if [ "$(STAGE)" = "prod" ]; then \
		echo "Pushing Docker image to repository ..."; \
		docker push $(IMAGE_TAG); \
	else \
		echo "Not in production stage. Pushing not allowed."; \
	fi

run: fmt ## Run the server locally
	cd ../rdm-integration-frontend && rm -rf ./dist && ng build --configuration development
	docker stop redis || true && docker rm redis || true && docker run -p 6379:6379 --name redis -d redis
	cd image && go run ./app 100

fmt: ## Format the go code
	cd image && go fmt ./app/...
