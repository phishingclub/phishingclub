#!/bin/sh
# entrypoint for production docker compose

# get appuser uid/gid dynamically, fallback to 1000 if not found
APP_UID=$(id -u appuser 2>/dev/null || echo 1000)
APP_GID=$(id -g appuser 2>/dev/null || echo 1000)

# create default config.json with correct paths if missing
if [ ! -f /app/config/config.json ]; then
  cat <<EOF > /app/config/config.json
{
  "acme": { "email": "" },
  "administration": {
    "tls_host": "localhost",
    "tls_auto": false,
    "tls_cert_path": "/app/config/certs/admin/public.pem",
    "tls_key_path": "/app/config/certs/admin/private.pem",
    "address": "0.0.0.0:8000",
    "ip_allow_list": []
  },
  "phishing": {
    "http": "0.0.0.0:8080",
    "https": "0.0.0.0:8443"
  },
  "database": {
    "engine": "sqlite3",
    "dsn": "file:/app/data/db.sqlite3"
  },
  "log": {
    "path": "",
    "errorPath": ""
  },
  "ip_security": {
    "admin_allowed": [],
    "trusted_proxies": [],
    "trusted_ip_header": ""
  }
}
EOF
fi

# if running as root, fix permissions and switch to appuser
if [ "$(id -u)" = "0" ]; then
  chown -R "$APP_UID:$APP_GID" /app/config /app/data 2>/dev/null || true
  exec su appuser -c "/app/phishingclub --config /app/config/config.json"
else
  exec /app/phishingclub --config /app/config/config.json
fi
