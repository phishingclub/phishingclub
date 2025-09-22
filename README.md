# Phishing Club

[![Latest Release](https://img.shields.io/github/v/release/phishingclub/phishingclub)](https://github.com/phishingclub/phishingclub/releases/latest)
[![Discord](https://img.shields.io/badge/Discord-Join%20Server-7289da?style=flat&logo=discord&logoColor=white)](https://discord.gg/Zssps7U8gX)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

The self-hosted phishing framework for penetration testing and security awareness training.

## Quick start

⚡ For systemd-enabled distributions, installation is quick and easy

Run the following on the server
```
curl -fsSL https://raw.githubusercontent.com/phishingclub/phishingclub/main/install.sh | bash
```

Remember to copy the admin URL and password

For a manual step by step guide or more in depth installation information - [click here](https://phishing.club/guide/management/#install)


## Features

- **Multi-stage phishing flows** - Put together multiple phishing pages
- **Reverse proxy phishing** - Capture sessions to bypass weak MFA
- **Domain proxying** - Configure domains to proxy and mirror content from target sites
- **Flexible scheduling** - Time windows, business hours, or manual delivery
- **Multiple domains** - Auto TLS, custom sites, asset management
- **Advanced delivery** - SMTP configs or custom API endpoints
- **Recipient tracking** - Groups, CSV import, repeat offender metrics
- **Analytics** - Timelines, dashboards, per-user event history
- **Automation** - HMAC-signed webhooks, REST API, import/export
- **Multi-tenancy** - Segregated client handling and statistics for service providers
- **Security features** - MFA, SSO, session management, IP filtering
- **Operational tools** - In-app updates, CLI installer, config management

### Resources & Learning
- [**HTML QR Codes in Phishing Emails**](https://www.linkedin.com/feed/update/urn:li:activity:7327787503921336320/) - Example of using a QR code in a phishing email. The QR code is made from HTML with inline styling.
- [**Setting up a domain with a website including a 404 page**](https://www.linkedin.com/feed/update/urn:li:activity:7328132395189067777) - Setup a domain with auto TLS with website content and 404 page.
- [**Custom delivery with rich pasting into 3. party**](https://www.linkedin.com/feed/update/urn:li:activity:7329121654842830849) - Custom lure delivery via. rich pasting into 3. party providers like gmail.
- [**Using multi-page phishing flow**](https://www.linkedin.com/feed/update/urn:li:activity:7329454798959808512) - Setting up a mutli page phishing flow. Great for both red team operations and simulations with training.
- [**Delivery using external API instead of SMTP**](https://www.linkedin.com/feed/update/urn:li:activity:7329565689025900544) - Example of how to use the API delivery. Makes it possible to deliver anywhere.

### Students & Learning

For cybersecurity students who want hands-on phishing simulation experience. The development setup includes a local mail server (Mailpit) so you can see emails in real-time without needing external SMTP. Spin up campaigns, test templates, and learn how phishing attacks work in a safe, contained environment.

## Template Development

### Phishing Template Workbench

Speed up your template development with our template workbench tool:

**[Phishing Template Workbench](https://github.com/phishingclub/templates)** - A developer-focused environment for creating and testing phishing simulation templates.

- **Preview** - Preview templates
- **Variable support** - See `{{.FirstName}}`, `{{.Email}}` substitution with realistic sample data
- **Naive Responsive Testing** - Preview templates across mobile, tablet, and desktop
- **Export Ready** - Compatible with Phishing Club formats
- **Included Templates** - Comes with example templates covering common phishing scenarios that you can import and customize




## Development Setup

This repository contains the core Phishing Club platform.

### Prerequisites

- Docker and Docker Compose
- Git
- Make (optional, for convenience commands)

### Quick Start

1. **Clone the repository:**
```bash
git clone https://github.com/phishingclub/phishingclub.git
cd phishingclub
```

2. **Start the services:**
```bash
make up
# or manually:
docker compose up -d
```

3. **Access the platform:**
- Administration: `http://localhost:8003`
- HTTP Phishing Server: `http://localhost:80`
- HTTPS Phishing Server: `https://localhost:443`

4. **Get admin credentials:**

The **username** and **password** are output in the terminal when you start the services. If you restart the backend service before completing setup by logging in, the username and password will change.

```bash
make backend-password
```

5. **Setup and start phishing:**

Open `https://localhost:8003` and setup the admin account using the credentials from step 4.

Visit the [Phishing Club Guide](https://phishing.club/guide/introduction/) for more information.

## Services and Ports

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
| 8105 | MITMProxy| MITMProxy web interface |
| 8106 | MITMProxy | MITMProxy external access |
| 8201 | ACME Server | Pebble ACME server for certificates |
| 8202 | ACME Management | Pebble management interface |


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

For development we use `.test` for all domains. But this must also be handled on the host level. You must either modify the hosts file and add the domains you use or run a local DNS server and ensure all *.test domains resolves to 127.0.0.1.

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

## License

Phishing Club is available under a dual licensing model:

### Open Source License (AGPL-3.0)
This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0). This means:
- ✅ You can use, modify, and distribute the software freely
- ✅ Perfect for educational, research, and commercial use
- ✅ You can run your own instance for security testing or professional services
- ⚠️ **Important**: If you provide the software modified as a network service, you must make your source code available under AGPL-3.0

### Commercial License
For organizations that want to:
- Use Phishing Club in commercial products without AGPL restrictions
- Offer Phishing Club as a service without source code disclosure
- Modify or modify the codebase without source code disclosure
- Get prioritized support

**Contact us for commercial licensing**: [license@phishing.club](mailto:license@phishing.club)

## Roadmap

There is no offical roadmap at this moment.

But you can vote with emojis on the `[feature]` requests [on Github](https://github.com/phishingclub/phishingclub) or even add you own feature request.

## Contributing

We welcome contributions from the community! Please read our [Contributing Guidelines](CONTRIBUTING.md) for detailed information on:

- Development setup and workflow
- Code standards and conventions
- Submission requirements
- License agreements

**Quick Start for Contributors:**
1. Check existing issues and create a feature request if needed
2. Wait for approval before starting work
3. Fork the repository and create a feature branch
4. Follow our development workflow and coding standards
5. Submit a pull request with signed commits

For complete details, see [CONTRIBUTING.md](CONTRIBUTING.md).


## Support

Need help? Join the [Phishing Club Discord](https://discord.gg/Zssps7U8gX)

- **General Support**: Join our Discord community or open a GitHub issue
- **Commercial Licensing**: Contact [license@phishing.club](mailto:license@phishing.club)
- **Security Issues**: See our [Security Policy](SECURITY.md)

## Security and Ethical Use

This platform is designed for **authorized security testing only**.

For important information about:
- Reporting security vulnerabilities
- Ethical use requirements
- Legal responsibilities
- Security best practices

Please read our [Security Policy](SECURITY.md).

**Important**: Users are solely responsible for ensuring their use complies with all applicable laws and regulations.
