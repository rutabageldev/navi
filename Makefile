ENV        ?= dev
VERSION    ?= $(shell cat .last-deployed-version)
SERVICE    ?= digest

.PHONY: setup setup-infra dev test lint build deploy smoketest \
        healthcheck rollback migrate vault-seed logs status \
        check-generated validate-schemas \
        renew-vault-token install-cron uninstall-cron

## setup: Install pre-commit hooks (run once after cloning)
setup:
	pre-commit install
	pre-commit install --hook-type commit-msg

## setup-infra: Create external Docker network on homelab node (one-time, idempotent)
setup-infra:
	docker network inspect navi >/dev/null 2>&1 || docker network create navi

## dev: Start local dev environment
dev:
	NAVI_ENV=dev docker compose -f docker-compose.dev.yml up

## test: Run unit tests with race detector
test:
	go work edit -json | jq -r '.Use[].DiskPath' | \
	    xargs -I{} go test -race ./{}/...

## lint: Run golangci-lint
lint:
	golangci-lint run ./services/digest/... ./services/internal/...

## build: Build Docker images for changed services since last tag
build:
	./scripts/build.sh $(VERSION)

## deploy: Deploy service to environment (emergency use; CI handles normal deploys)
deploy:
	./scripts/deploy.sh $(ENV) $(VERSION) $(SERVICE)

## smoketest: Run smoke test suite against environment
smoketest:
	go run ./services/digest/cmd/smoketest/... \
	    -env $(ENV) \
	    -addr $$(./scripts/service-addr.sh $(ENV) $(SERVICE))

## healthcheck: Run health checks against environment
healthcheck:
	./scripts/healthcheck.sh $(ENV) $(SERVICE)

## rollback: Emergency rollback — make rollback ENV=x VERSION=y SERVICE=z
rollback:
	./scripts/rollback.sh $(ENV) $(VERSION) $(SERVICE)

## migrate: Run pending migrations against environment
migrate:
	go run ./services/digest/cmd/migrate/... -env $(ENV)

## vault-seed: Seed Vault paths with placeholder values for environment
vault-seed:
	./scripts/vault-seed.sh $(ENV)

## logs: Tail container logs for environment
logs:
	docker compose -f docker-compose.$(ENV).yml logs -f

## status: Show running container status across all environments
status:
	docker compose -f docker-compose.dev.yml ps 2>/dev/null || true
	docker compose -f docker-compose.staging.yml ps 2>/dev/null || true
	docker compose -f docker-compose.yml ps 2>/dev/null || true

## check-generated: Verify oapi-codegen output is current
check-generated:
	./scripts/check-generated.sh

## validate-schemas: Validate all event JSON Schema files
validate-schemas:
	./scripts/validate-schemas.sh

## renew-vault-token: Renew the Navi Vault token manually
renew-vault-token:
	@scripts/renew-vault-token.sh

## install-cron: Install automated weekly Vault token renewal cron job
install-cron:
	@(crontab -l 2>/dev/null | grep -v 'renew-vault-token'; \
	  echo "0 6 * * 1 /opt/navi/scripts/renew-vault-token.sh >> /var/log/navi-vault-renewal.log 2>&1") \
	  | crontab -
	@echo "Cron job installed: weekly renewal every Monday at 06:00"

## uninstall-cron: Remove automated Vault token renewal cron job
uninstall-cron:
	@crontab -l 2>/dev/null | grep -v 'renew-vault-token' | crontab -
	@echo "Cron job removed"
