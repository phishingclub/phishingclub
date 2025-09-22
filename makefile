.PHONY: build down up fix-tls backend-purge backend-down purge logs backend-password dbgate-down dbgate-up
up:
	sudo docker compose up -d backend frontend api-test-server pebble dbgate mailer dozzle stats dns test mitmproxy; \
	sudo docker compose logs -f --tail 1000 backend frontend;
down:
	-sudo docker compose down --remove-orphans
up-build:
	sudo docker compose up --build --force
up-reset: down purge up
restart: down up
prune:
	sudo docker system prune -a
docker-reset: down up
ps:
	sudo docker compose ps
logs:
	sudo docker compose logs -f --tail 1000 backend frontend
logs-all:
	sudo docker compose logs -f
purge:
	sudo rm -rf ./backend/.dev/*
fix-tls:
	sudo docker compose exec backend bash -c "rm -rf /app/certs/acme/*"
	sudo rm -rf ./backend/certs/acme/*

# backend
backend-restart:
	sudo docker compose stop backend; \
	sudo docker compose up -d backend; \
	sudo docker compose logs -f --tail 1000 backend;
backend:
	sudo docker compose up -d; \
	sudo docker compose logs -f --tail 1000 backend;
backend-clear-certs:
	sudo docker compose exec backend rm -rf /app/certs/acme
backend-attach:
	sudo docker compose exec backend bash
backend-logs:
	sudo docker compose logs -f --tail 1000 backend
backend-build:
	sudo docker compose build backend
backend-down:
	sudo docker compose down backend;
backend-up:
	sudo docker compose up backend -d;
backend-reset:
	-sudo docker compose stop backend; \
	sudo docker compose rm -force -v backend; \
	sudo docker compose up -d backend; \
	sudo docker compose logs -f --tail 1000 backend;
backend-db-reset:
	sudo docker compose stop dbgate; \
	sudo rm -f ./backend/.dev/db.sqlite3; \
	sudo docker compose exec backend bash -c "rm -rf /app/.dev/db.sqlite3";
	sudo docker compose stop backend; \
	sudo rm -rf ./backend/.dev/*
	touch -c ./backend/.dev/db.sqlite3; \
	sudo docker compose start dbgate; \
	sudo docker compose up -d backend;
backend-password:
	@echo "Finding password"; sudo docker compose logs backend | grep -F "Password:" | tail -n 1
backend-recover-password:
	sudo docker compose exec -it backend sh -c "cd /app/.dev-air; ./phishingclub -files /app/.dev -config /app/config.docker.json -recover"

# frontend
frontend:
	sudo docker compose up -d; \
	sudo docker compose logs -f --tail 1000 frontend;
frontend-build:
	-sudo docker compose stop frontend; \
	sudo docker compose rm --force -v frontend; \
	sudo docker compose up -d frontend;
frontend-restart:
	sudo docker compose restart frontend
frontend-attach:
	sudo docker compose exec frontend bash
frontend-logs:
	sudo docker compose logs -f --tail 1000 frontend

# dbgate
dbgate-restart:
	sudo docker compose restart dbgate; 
dbgate-up:
	sudo docker compose start dbgate; 
dbgate-down:
	sudo docker compose stopdbgate; 

# pebble
pebble-attach:
	sudo docker compose exec pebble sh

# dns
dns-attach:
	sudo docker compose exec dns sh
dns-logs:
	sudo docker compose logs -f --tail 1000 dns
dns-restart:
	sudo docker compose restart dns
dns-rebuild:
	sudo docker compose stop dns; \
	sudo docker compose rm -force -v dns; \
	sudo docker compose up -d dns; \
	sudo docker compose logs -f --tail 1000 dns;

# api-test-server
api-test-server-build:
	sudo docker compose build api-test-server
	sudo docker compose up -d api-test-server
api-test-server-logs:
	sudo docker compose logs -f --tail 1000 api-test-server
api-test-server-restart:
	sudo docker compose restart api-test-server

# utils
utils-attach:
	sudo docker compose exec test /bin/bash

# mailer
mailer-logs:
	sudo docker compose logs -f --tail 1000 mailer
mailer-restart:
	sudo docker compose restart mailer

# stats
stats-logs:
	sudo docker compose logs -f --tail 1000 stats
stats-restart:
	sudo docker compose restart stats

# dozzle
dozzle-logs:
	sudo docker compose logs -f --tail 1000 dozzle
dozzle-restart:
	sudo docker compose restart dozzle

# mitmproxy
mitmproxy-logs:
	sudo docker compose logs -f --tail 1000 mitmproxy
mitmproxy-restart:
	sudo docker compose restart mitmproxy
mitmproxy-up:
	sudo docker compose up -d mitmproxy
mitmproxy-down:
	sudo docker compose stop mitmproxy
mitmproxy-attach:
	sudo docker compose exec mitmproxy sh
mitmproxy-reset:
	sudo docker compose stop mitmproxy; \
	sudo docker compose rm -f mitmproxy; \
	sudo docker compose up -d mitmproxy; \
	sudo docker compose logs -f --tail 1000 mitmproxy;
mitmproxy-token:
	sudo docker compose logs mitmproxy | grep -i "web server listening" | tail -1 || echo "Token not found - try: make mitmproxy-logs"
mitmproxy-password:
	@echo "Latest mitmproxy password/token:"; sudo docker compose logs mitmproxy | grep -oE "token=[a-zA-Z0-9]+" | tail -1 | cut -d= -f2 || echo "Password not found - make sure mitmproxy is running"
mitmproxy-url:
	@echo "mitmproxy web interface URL:"; sudo docker compose logs mitmproxy | grep -oE "http://0\.0\.0\.0:8080/\?token=[a-zA-Z0-9]+" | tail -1 | sed 's/0\.0\.0\.0:8080/localhost:8105/' || echo "URL not found - make sure mitmproxy is running"
mitmproxy-purge:
	sudo docker compose stop mitmproxy; \
	sudo docker compose rm -f mitmproxy; \
	sudo docker volume rm -f phishingclub_mitmproxy_data; \
	sudo docker compose up -d mitmproxy; \
	sudo docker compose logs -f --tail 1000 mitmproxy;
