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

We welcome contributions from the community! Please follow our contribution guidelines:

### Before Contributing

1. **Check existing issues** - Search for existing feature requests or bug reports
2. **Create a feature request** - If your idea doesn't exist, create a detailed feature request issue, we have criteria for which features we want to add and do not waste anyones time with feature requests we never wanted.
3. **Wait for approval** - Allow us to review and approve your proposal
4. **Discuss implementation** - We may suggest changes or alternative approaches

### Development Workflow

1. **Fork the repository** and clone your fork
2. **Create a feature branch** from `main`:
   ```bash
   git checkout -b feat/your-feature-name
   ```
3. **Follow naming conventions**:
   - Features: `feat/feature-name`
   - Bug fixes: `fix/bug-description`
   - Documentation: `docs/update-description`
   - Refactoring: `refactor/component-name`

4. **Follow conventions**:
   - Follow existing code style and patterns
   - Update documentation as needed

5. **Prepare for submission**:
   - **Rebase your commits** to a single, clean commit before creating the pull request
   - **Sign your commit** using the `-s` flag: `git commit -s -m "Your commit message"`
   - Ensure your commit message is clear and descriptive

6. **Submit a pull request**:
   - Reference the related issue number
   - Provide a clear description of changes
   - Include screenshots/videos for UI changes

### Code Standards

- **Formatting**: Use project configurations
- **Documentation**: Update relevant docs with your changes
- **Security**: Follow secure coding practices

### License Agreement

**Important**: All contributors must agree to our Contributor License Agreement (CLA).

By contributing to Phishing Club, you agree that your contributions will be licensed under the same dual license terms (AGPL-3.0 and commercial). You confirm that:

- You have the right to contribute the code
- Your contributions are your original work or properly attributed
- You grant Phishing Club the right to license your contributions under both AGPL-3.0 and commercial licenses

**Required**:
- All commits must be signed off using the `-s` flag: `git commit -s -m "Your commit message"`
- Before submitting a pull request, rebase your branch to a single commit
- Use descriptive commit messages that explain what and why

```bash
# Example workflow:
git rebase -i main    # Interactive rebase against main branch to squash commits
git commit --amend -s # Add sign-off to the final commit if needed
```

This adds a "Signed-off-by" line indicating you agree to our [CLA](CLA.md) and the [Developer Certificate of Origin](https://developercertificate.org/).

For detailed terms, see:
- [Contributor License Agreement (CLA.md)](CLA.md)
- [Contributors Guide (CONTRIBUTORS.md)](CONTRIBUTORS.md)


## Support and Security

Need help, join the [Phishing Club Discord](https://discord.gg/Zssps7U8gX)

- **Security Issues**: Report privately via [security@phishing.club](mailto:security@phishing.club)
- **Commercial Licensing**: Contact [license@phishing.club](mailto:license@phishing.club)
- **General Support**: Join our Discord community or open a GitHub issue

## Only for ethical use

This platform is designed for authorized security testing only. Users are responsible for:

- Obtaining proper authorization before conducting phishing simulations
- Complying with all applicable laws and regulations
- Using the platform ethically and responsibly
- Protecting any data collected during testing

This tool is for authorized security testing only. Misuse of this software may violate applicable laws. Users are solely responsible for ensuring their use complies with all applicable laws and regulations.
