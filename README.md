# Phishing Club

[![Latest Release](https://img.shields.io/github/v/release/phishingclub/phishingclub)](https://github.com/phishingclub/phishingclub/releases/latest)
[![Discord](https://img.shields.io/badge/Discord-Join%20Server-7289da?style=flat&logo=discord&logoColor=white)](https://discord.gg/Zssps7U8gX)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

**Phishing Club** is a phishing simulation, training and red team phishing framework.

![Phishing Club Dashboard](https://phishing.club/img/animated.gif)

## Quick start (production)

⚡ For systemd-enabled distributions, installation is quick and easy

Run the following on the server
```
curl -fsSL https://raw.githubusercontent.com/phishingclub/phishingclub/main/install.sh | bash
```

Remember to copy the admin URL and password

[Manual installation](https://phishing.club/guide/management/#install)

[GHCR Images](https://github.com/phishingclub/phishingclub/pkgs/container/phishingclub)

[Production Docker Compose example](https://github.com/phishingclub/phishingclub/blob/develop/docker-compose.production.yml)

## Features

Phishing Club provides a lot of features for simulation and red teaming, such as:

- **Multi-stage phishing flows** - Put together multiple phishing pages
- **Domain proxying** - Configure domains to proxy and mirror content from target sites
- **Flexible scheduling** - Time windows, business hours, or manual delivery
- **Multiple domains** - Auto TLS, custom sites and asset management
- **Advanced delivery** - SMTP configs or custom API Sender with OAuth support
- **Recipient tracking** - Groups, CSV import, SCIM provisioning, repeat offender metrics
- **Awareness training** - Run training campaigns that record started and completed, kept separate from phishing risk
- **Campaign reports** - PDF export with customizable HTML templates for phishing and training, automatically emailed on completion
- **Analytics** - Timelines, dashboards, per-user event history
- **Automation** - HMAC-signed webhooks, REST API, import/export
- **Multi-tenancy** - Segregated client handling and statistics for service providers
- **Anonymization** - Pseudonymized campaigns, automatic anonymization on close and retention windows for compliance
- **Branding** - Replace logos and login image with your own
- **Security features** - MFA, SSO (Entra ID and OIDC), session management, IP filtering
- **Operational tools** - In-app updates, CLI installer, config management

## AiTM and Red Team Features

- **Reverse proxy phishing** - Capture sessions to bypass weak MFA, import captured cookies with the Session Sushi extension
- **Remote browser phishing** - Stream and interact with a victim's live browser session
- **Full control** - Modify and capture requests and responses independently
- **DOM rewriting** - Modify content using CSS/jQuery-like selectors or regex
- **Path and param rewriting** - Rewrite URL paths and query parameters on the fly
- **Dynamic obfuscation** - Avoid static detection with dynamically obfuscated landing pages
- **Evasion page** - Customize the pre-lure evasion page
- **Custom deny page** - Decide what bots or evaded visitors see
- **Access control** - Default deny-list until visiting phishing lure URL
- **Advanced filtering** - Use JA4, CIDR and geo-IP to control lure URL access
- **Browser impersonation** - Impersonate JA4 fingerprints in proxied requests
- **Response overwriting** - Shortcut proxying with custom responses
- **Forward proxying** - Use HTTP and SOCKS5 proxies to ensure requests originate from the right location
- **Visual Editor** - Use the visual editor to easily setup a proxy
- **Import compromised OAuth token** - Use compromised tokens to send more phishing via OAuth enabled endpoints
- **Device Code phishing** - Device code phishing is as simple as adding a single line to a email or landing page

### Blogs & Resources
- [Phishing Club User Guide](https://phishing.club/guide/)
- [Covert red team phishing with Phishing Club](http://phishing.club/blog/covert-red-team-phishing-with-phishing-club/)
- [Phishing Simulation vs Red Team Phishing: Understanding Different Approaches](https://phishing.club/blog/phishing-simulation-vs-red-team-phishing/)
- [Remote Browser Phishing with Phishing Club](https://phishing.club/blog/remote-browser-phishing/)


Wrote a blog post or write up about Phishing Club? Tell us about it and we might add it here. Reach out via a GitHub issue, discord or find our email :)

## Template Development

### Phishing Template Workbench

Speed up your template development with our template workbench tool:

**[Phishing Template Workbench](https://github.com/phishingclub/templates)** - A developer-focused environment for creating and testing phishing simulation templates.

- **Preview** - Preview templates
- **Variable support** - See `{{.FirstName}}`, `{{.Email}}` substitution with realistic sample data
- **Naive Responsive Testing** - Preview templates across mobile, tablet, and desktop
- **Export Ready** - Compatible with Phishing Club formats
- **Included Templates** - Comes with example templates covering common phishing scenarios that you can import and customize

## Development

Run the whole stack locally with Docker and make:

```bash
git clone https://github.com/phishingclub/phishingclub.git
cd phishingclub
make up
make backend-password
```

Then open `https://localhost:8003` and setup the admin account.

For prerequisites, service ports, make commands, local DNS and SSL setup, see [DEVELOPMENT.md](DEVELOPMENT.md). For the contribution process, see [CONTRIBUTING.md](CONTRIBUTING.md).

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
- Modify the codebase without source code disclosure

**Contact for commercial licensing**: [license@phishing.club](mailto:license@phishing.club)

## Roadmap

There is no official roadmap.

But you can vote with emojis on the `[feature]` requests [on Github](https://github.com/phishingclub/phishingclub/issues?q=is%3Aissue+label%3Afeature) or add your own feature request.

Feature request with a high number of votes will be prioritized, however it is no guaranteed they will be implemented. Ultimately what gets implemented, how and when highly depends on [me](https://github.com/ronniskansing) and what I think is right for the project.

## Contributing

We welcome contributions from the community! Please read our [Contributing Guidelines](CONTRIBUTING.md) 

**Quick Start for Contributors:**
1. Check existing issues and create a feature request if needed
2. Wait for approval before starting work
3. Fork the repository and create a feature branch
4. Follow our development workflow and coding standards
5. Submit a pull request with signed commits

For complete details, see [CONTRIBUTING.md](CONTRIBUTING.md).

**Suggestions for Contributors**
- Improve or add templates to the [template project](https://github.com/phishingclub/templates)
- Check existing feature requests - Want to work on something, make a comment.


## Support

Need help? Join the [Phishing Club Discord](https://discord.gg/Zssps7U8gX)


Community support is provided on a best-effort, volunteer basis. For dedicated assistance, paid support is available.

- **General Support**: Join our Discord community or open a GitHub issue
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
