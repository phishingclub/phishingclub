# Phishing Club

[![Latest Release](https://img.shields.io/github/v/release/phishingclub/phishingclub)](https://github.com/phishingclub/phishingclub/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/phishingclub/phishingclub/total)](https://github.com/phishingclub/phishingclub/releases)
[![Discord](https://img.shields.io/badge/Discord-Join%20Server-7289da?style=flat&logo=discord&logoColor=white)](https://discord.gg/Zssps7U8gX)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

</div>

The self-hosted phishing framework for security awareness training and penetration testing.

## Overview

Phishing Club is a phishing simulation framework designed for security professionals, red teams, and organizations looking to test and improve their security awareness. This platform provides tools for creating, deploying, and managing phishing campaigns in a controlled environment.

See the [LICENSE](LICENSE) file for the full AGPL-3.0 terms.

### Features

- Setup reusable phishing templates with emails, multiple phishing page flow, custom identifiers and more
- Advanced scheduling with time-boxed sending and daily schedules (like monday, wednesday between 08:00 and 16:00)
- API Sending, don't be limited by SMTP, deliver directly via an API
- Handle multiple domains with automatic TLS (https)
- Campaign overview and stats with graphs, per-recipient events and more
- Dashboard with company statistics, trendlines, calendar and overview
- Recipients and groups with stats such as repeat offender tracking
- Custom websites and 404 pages
- Asset management (upload js, css, images or whatever you want)
- Security features like MFA, audit logging, session management, IP locked sessions and more
- Webhooks for integrating with 3rd parties
- MSSP Ready with individual dashboard and etc. for each client.
- In-app updates

And lots lots more!

## Getting Started

### Install

Download the latest release and follow our installation guide:

1. **Download the latest version** from [GitHub Releases](https://github.com/phishingclub/phishingclub/releases)
2. **Follow the installation guide** at [https://phishing.club/guide/management/#install](https://phishing.club/guide/management/#install)
3. **Complete the setup** by following the step-by-step instructions in our documentation

For detailed setup instructions, troubleshooting, and best practices, visit the [Phishing Club Guide](https://phishing.club/guide/introduction/).

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
| 8102 | Mail Server | Mailpit SMTP server for testing |
| 8103 | Container Logs | Dozzle log viewer |
| 8104 | Container Stats | Docker container statistics |
| 8201 | ACME Server | Pebble ACME server for certificates |
| 8202 | ACME Management | Pebble management interface |

## Development Commands

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

All domains ending with `.test` are automatically handled by the development setup. To use custom domains during development:

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
127.0.0.1 vikings.test
127.0.0.1 dark-water.test
```

## Configuration

### Environment Variables

Copy the example environment file and customize:
```bash
cp backend/.env.example backend/.env.development
```

Key configuration options:
- Database settings
- SMTP configuration
- Domain settings
- Security keys

### SSL Certificates

The development environment uses Pebble ACME server for automatic SSL certificate generation. In production, configure your preferred ACME provider or upload custom certificates.


## License

Phishing Club is available under a dual licensing model:

### Open Source License (AGPL-3.0)
This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0). This means:
- ✅ You can use, modify, and distribute the software freely
- ✅ Perfect for educational, research, and non-commercial use
- ✅ You can run your own instance for internal security testing
- ⚠️ **Important**: If you provide the software as a network service (SaaS), you must make your source code available under AGPL-3.0

### Commercial License
For organizations that want to:
- Use Phishing Club in commercial products without AGPL restrictions
- Offer Phishing Club as a service without source code disclosure
- Integrate with proprietary software
- Get dedicated support and maintenance

**Contact us for commercial licensing**: [license@phishing.club](mailto:license@phishing.club)

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
