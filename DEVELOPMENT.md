# Development

How to run Phishing Club locally. For the contribution process (branching, signed commits, pull requests) see [CONTRIBUTING.md](CONTRIBUTING.md).

The development environment is also a safe, contained place to learn hands-on phishing. Spin up campaigns, test templates, and inspect traffic. It comes with containers for local SMTP/mailbox, a MITMProxy for viewing traffic towards proxied sites, and everything else you need. Need help? Join the [Discord](https://discord.gg/Zssps7U8gX).

## Prerequisites

- Docker and Docker Compose
- Git
- Make (recommended, the development workflow is built around make)
- Enough memory for the first build. The first start compiles the whole backend
  dependency graph and installs the frontend, which is memory heavy. On a machine
  with limited memory these two steps running at once can be killed by the OOM
  killer, showing up as `signal: killed` on the backend and `vite: not found` on the
  frontend. On such machines start with `make up-low-mem`, which builds the backend
  before starting the frontend so the heavy steps do not overlap. Otherwise use the
  normal `make up`, which builds everything in parallel for the fastest startup.

## Quick Start

1. **Clone the repository:**
```bash
git clone https://github.com/phishingclub/phishingclub.git
cd phishingclub
```

2. **Start the services:**
```bash
make up
```

Wait for the backend to finish starting before continuing. Follow the startup with `make logs`.

3. **Access the platform:**
- Administration: `https://localhost:8003`
- HTTP Phishing Server: `http://localhost:80`
- HTTPS Phishing Server: `https://localhost:443`

4. **Get admin credentials:**

The **username** and **password** are output in the terminal when you start the services. If you restart the backend service before completing setup by logging in, the username and password will change.

```bash
make backend-password
```

`make backend-password` outputs the password from the latest setup, so you can use it instead of scrolling back through the logs.

5. **Setup and start phishing:**

Open `https://localhost:8003` and setup the admin account using the credentials from step 4.

Visit the [Phishing Club Guide](https://phishing.club/guide/introduction/) for more information.

## Development Services and Ports

| Port | Service | Description |
|------|---------|-------------|
| 80 | HTTP Phishing Server | HTTP phishing server for campaigns |
| 443 | HTTPS Phishing Server | HTTPS phishing server with SSL |
| 8002 | Backend API | Backend API server |
| 8003 | Frontend | Development frontend with Vite |
| 8101 | Database Viewer | DBGate database administration |
| 8102 | Mail Server | Mailpit SMTP server with SpamAssassin integration |
| 8103 | Container Logs | Dozzle log viewer |
| 8104 | Container Stats | Docker container statistics |
| 8105 | MITMProxy | MITMProxy web interface |
| 8106 | MITMProxy | MITMProxy external access |
| 8107 | API Test Server | Test endpoint for the API Sender |
| 8201 | ACME Server | Pebble ACME server for certificates |
| 8202 | ACME Management | Pebble management interface |
| 8203 | ACME Challenge | Pebble challenge test server |


## Development Commands

The `makefile` has a lot of convenience commands for development.

```bash
# Start all services
make up

# Stop all services
make down

# View logs
make logs

# Restart specific service
make backend-restart
make frontend-restart

# Access service containers
make backend-attach
make frontend-attach

# Reset backend database
make backend-db-reset

# Get backend admin password
make backend-password
```

## Development Domains

For development we use `.test` for all domains.

The Docker Compose stack includes a DNSMasq container that resolves `.test` domains on the internal Docker network, so the containers can reach each other. This does not cover your host. You must ALSO handle resolution on your own machine, either by adding the `.test` domains you use to your hosts file or by running a local DNS server that resolves all `*.test` domains to 127.0.0.1.

### Option 1: DNSMasq (Recommended)
```bash
# Add to your DNSMasq configuration
address=/.test/127.0.0.1
```

### Option 2: Hosts File
Add to `/etc/hosts`:
```
127.0.0.1 microsoft.test
127.0.0.1 google.test
... add your development domains here
```

## Development SSL Certificates

The development environment uses Pebble ACME server for automatic SSL certificate generation. In production, configure your preferred ACME provider or upload custom certificates.

If you experience any issues with certificate generation, bring the backend down,
clear the local certs and start the backend again:

 - `make backend-down`
 - `make backend-clear-certs`
 - `make backend-up`

## Certificate warning
When developing it can be nice to ignore certificate warnings, especially when handling complex proxy setups. Use a
dedicated browser and skip certificate warning.

On Ubuntu you can add custom shortcut for chromium without cert warnings.

`~/.local/share/applications/chromium-dev.desktop`
```
[Desktop Entry]
Version=1.0
Type=Application
Name=Chromium Phishing Dev
Comment=Chromium for development with SSL certificate errors ignored
Exec=chromium-browser --ignore-certificate-errors --incognito
Icon=chromium-browser
Terminal=false
```
