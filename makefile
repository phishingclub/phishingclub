# sudo only if docker needs it on this host
NEEDS_SUDO := $(shell docker info >/dev/null 2>&1 || { [ "$$(uname)" = "Linux" ] && echo sudo; })
DOCKER := $(NEEDS_SUDO) docker
SUDO := $(NEEDS_SUDO)

.PHONY: build down up up-low-mem fix-tls backend-purge backend-down purge logs backend-password dbgate-down dbgate-up geoip-fetch govulncheck test-proxy test-proxy-fast test-lure test-lure-fast
up:
	$(DOCKER) compose up -d backend frontend api-test-server pebble dbgate mailer dns test mitmproxy; \
	$(DOCKER) compose logs -f --tail 1000 backend frontend;
down:
	-$(DOCKER) compose down --remove-orphans
up-build:
	$(DOCKER) compose up --build --force
# build a single, self contained binary with the frontend embedded -> build/phishingclub
# override arch: make build BIN_ARCH=arm64 ; override version: make build VERSION=1.2.3
build:
	BIN_ARCH=$(BIN_ARCH) VERSION=$(VERSION) ./backend/build_scripts/build.sh
# same as up but for machines with limited memory, the frontend waits for the backend
# build to finish so the two heavy first build steps do not run at the same time
up-low-mem:
	$(DOCKER) compose -f docker-compose.yml -f docker-compose.low-mem.yml up -d backend frontend api-test-server pebble dbgate mailer dns test mitmproxy; \
	$(DOCKER) compose -f docker-compose.yml -f docker-compose.low-mem.yml logs -f --tail 1000 backend frontend;
up-reset: down purge up
restart: down up
prune:
	$(DOCKER) system prune -a
docker-reset: down up
ps:
	$(DOCKER) compose ps
logs:
	$(DOCKER) compose logs -f --tail 1000 backend frontend
logs-all:
	$(DOCKER) compose logs -f
purge:
	$(SUDO) rm -rf ./backend/.dev/*
fix-tls:
	$(DOCKER) compose exec backend bash -c "rm -rf /app/certs/acme/*"
	$(SUDO) rm -rf ./backend/certs/acme/*

# backend
backend-restart:
	$(DOCKER) compose stop backend; \
	$(DOCKER) compose up -d backend; \
	$(DOCKER) compose logs -f --tail 1000 backend;
backend:
	$(DOCKER) compose up -d; \
	$(DOCKER) compose logs -f --tail 1000 backend;
backend-clear-certs:
	$(DOCKER) compose exec backend rm -rf /app/certs/acme
backend-attach:
	$(DOCKER) compose exec backend bash
backend-logs:
	$(DOCKER) compose logs -f --tail 1000 backend
backend-build:
	$(DOCKER) compose build backend
backend-down:
	$(DOCKER) compose down backend;
backend-up:
	$(DOCKER) compose up backend -d;
backend-reset:
	-$(DOCKER) compose stop backend; \
	$(DOCKER) compose rm -force -v backend; \
	$(DOCKER) compose up -d backend; \
	$(DOCKER) compose logs -f --tail 1000 backend;
backend-db-reset:
	$(DOCKER) compose stop dbgate; \
	$(SUDO) rm -f ./backend/.dev/db.sqlite3; \
	$(DOCKER) compose exec backend bash -c "rm -rf /app/.dev/db.sqlite3";
	$(DOCKER) compose stop backend; \
	$(SUDO) rm -rf ./backend/.dev/*
	touch -c ./backend/.dev/db.sqlite3; \
	$(DOCKER) compose start dbgate; \
	$(DOCKER) compose up -d backend;
backend-password:
	@echo "Finding password"; $(DOCKER) compose logs backend | grep -F "Password:" | tail -n 1
backend-recover-password:
	$(DOCKER) compose exec -it backend sh -c "cd /app/.dev-air; ./phishingclub -files /app/.dev -config /app/config.docker.json -recover"

# frontend
frontend:
	$(DOCKER) compose up -d; \
	$(DOCKER) compose logs -f --tail 1000 frontend;
frontend-build:
	-$(DOCKER) compose stop frontend; \
	$(DOCKER) compose rm --force -v frontend; \
	$(DOCKER) compose up -d frontend;
frontend-restart:
	$(DOCKER) compose restart frontend
frontend-attach:
	$(DOCKER) compose exec frontend bash
frontend-logs:
	$(DOCKER) compose logs -f --tail 1000 frontend

# dbgate
dbgate-restart:
	$(DOCKER) compose restart dbgate;
dbgate-up:
	$(DOCKER) compose start dbgate;
dbgate-down:
	$(DOCKER) compose stopdbgate;

# pebble
pebble-attach:
	$(DOCKER) compose exec pebble sh

# dns
dns-attach:
	$(DOCKER) compose exec dns sh
dns-logs:
	$(DOCKER) compose logs -f --tail 1000 dns
dns-restart:
	$(DOCKER) compose restart dns
dns-rebuild:
	$(DOCKER) compose stop dns; \
	$(DOCKER) compose rm -force -v dns; \
	$(DOCKER) compose up -d dns; \
	$(DOCKER) compose logs -f --tail 1000 dns;

# api-test-server
api-test-server-build:
	$(DOCKER) compose build api-test-server
	$(DOCKER) compose up -d api-test-server
api-test-server-logs:
	$(DOCKER) compose logs -f --tail 1000 api-test-server
api-test-server-restart:
	$(DOCKER) compose restart api-test-server

# utils
utils-attach:
	$(DOCKER) compose exec test /bin/bash

# mailer
mailer-logs:
	$(DOCKER) compose logs -f --tail 1000 mailer
mailer-restart:
	$(DOCKER) compose restart mailer

# mitmproxy
mitmproxy-logs:
	$(DOCKER) compose logs -f --tail 1000 mitmproxy
mitmproxy-restart:
	$(DOCKER) compose restart mitmproxy
mitmproxy-up:
	$(DOCKER) compose up -d mitmproxy
mitmproxy-down:
	$(DOCKER) compose stop mitmproxy
mitmproxy-attach:
	$(DOCKER) compose exec mitmproxy sh
mitmproxy-reset:
	$(DOCKER) compose stop mitmproxy; \
	$(DOCKER) compose rm -f mitmproxy; \
	$(DOCKER) compose up -d mitmproxy; \
	$(DOCKER) compose logs -f --tail 1000 mitmproxy;
mitmproxy-token:
	$(DOCKER) compose logs mitmproxy | grep -i "web server listening" | tail -1 || echo "Token not found - try: make mitmproxy-logs"
mitmproxy-password:
	@echo "Latest mitmproxy password/token:"; $(DOCKER) compose logs mitmproxy | grep -oE "token=[a-zA-Z0-9]+" | tail -1 | cut -d= -f2 || echo "Password not found - make sure mitmproxy is running"
mitmproxy-url:
	@echo "mitmproxy web interface URL:"; $(DOCKER) compose logs mitmproxy | grep -oE "http://0\.0\.0\.0:8080/\?token=[a-zA-Z0-9]+" | tail -1 | sed 's/0\.0\.0\.0:8080/localhost:8105/' || echo "URL not found - make sure mitmproxy is running"
mitmproxy-purge:
	$(DOCKER) compose stop mitmproxy; \
	$(DOCKER) compose rm -f mitmproxy; \
	$(DOCKER) volume rm -f phishingclub_mitmproxy_data; \
	$(DOCKER) compose up -d mitmproxy; \
	$(DOCKER) compose logs -f --tail 1000 mitmproxy;

# geoip
geoip-fetch:
	@echo "Fetching GeoIP data from ipverse/rir-ip..."; \
	cd backend/scripts && go run fetch-geoip-data.go

# tests
# run the proxy package tests in a standalone go container using the vendored
# dependencies, so it works without the dev stack running. the named volume
# keeps the go build cache between runs so only the first run does a full
# compile. the first run also pulls golang:1.25.10 and can take a few minutes;
# go test prints nothing until the build finishes, so silence is expected.
test-proxy:
	$(DOCKER) run --rm \
		-e GOCACHE=/gocache \
		-v $(CURDIR)/backend:/app \
		-v phishingclub_gocache:/gocache \
		-w /app \
		golang:1.25.10 \
		go test ./proxy/... -v

# same tests but inside the already running backend container, which has a warm
# build cache. much faster, but requires the dev stack to be up (make up)
test-proxy-fast:
	$(DOCKER) compose exec -w /app backend go test ./proxy/... -v

# lure URL code generation and the path segment helpers the request resolver
# uses. same standalone container approach as test-proxy above.
test-lure:
	$(DOCKER) run --rm \
		-e GOCACHE=/gocache \
		-v $(CURDIR)/backend:/app \
		-v phishingclub_gocache:/gocache \
		-w /app \
		golang:1.25.10 \
		go test ./lure/... ./server/... -v

# same tests inside the already running backend container
test-lure-fast:
	$(DOCKER) compose exec -w /app backend go test ./lure/... ./server/... -v

# security
govulncheck:
	$(DOCKER) run --rm \
		-v $(CURDIR)/backend:/app \
		-w /app \
		golang:1.25.10 \
		sh -c "go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./... -show verbose"
