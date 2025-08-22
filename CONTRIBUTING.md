# Contributing to Phishing Club

We welcome contributions from the community! Please follow these guidelines to ensure a smooth contribution process.

## Before Contributing

1. **Check existing issues** - Search for existing feature requests or bug reports
2. **Create a feature request** - If your idea doesn't exist, create a detailed feature request issue. We have criteria for which features we want to add.
3. **Wait for approval** - Allow us to review and approve your proposal
4. **Discuss implementation** - We may suggest changes or alternative approaches

## Development Workflow

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
   - Add tests when possible

5. **Prepare for submission**:
   - **Rebase your commits** to a single, clean commit before creating the pull request
   - **Sign your commit** using the `-s` flag: `git commit -s -m "Your commit message"`
   - Ensure your commit message is clear and descriptive

6. **Submit a pull request**:
   - Reference the related issue number
   - Provide a clear description of changes

## Code Standards

- **Formatting**: Use project configurations
- **Documentation**: Update relevant docs with your changes
- **Security**: Follow secure coding practices

## License Agreement

**Important**: All contributors must agree to our Contributor License Agreement (CLA).

By contributing to Phishing Club, you agree that your contributions will be licensed under the same dual license terms (AGPL-3.0 and commercial). You confirm that:

- You have the right to contribute the code
- Your contributions are your original work or properly attributed
- You grant Phishing Club the right to license your contributions under both AGPL-3.0 and commercial licenses

## Required Commit Practices

**All commits must be signed off** using the `-s` flag: `git commit -s -m "Your commit message"`

**Before submitting a pull request**, rebase your branch to a single commit:

```bash
# Example workflow to squash commits:
git rebase -i main    # Interactive rebase to squash commits
git commit --amend -s # Add sign-off to the final commit if needed
```

This adds a "Signed-off-by" line indicating you agree to our [CLA](CLA.md) and the [Developer Certificate of Origin](https://developercertificate.org/).

Use descriptive commit messages that explain what and why, not just what.

## Development Resources

For detailed terms and additional information, see:
- [Contributor License Agreement (CLA.md)](CLA.md)
- [Contributors Guide (CONTRIBUTORS.md)](CONTRIBUTORS.md)
- [Security Policy (SECURITY.md)](SECURITY.md)

## Getting Help

- **General Questions**: Join our [Discord community](https://discord.gg/Zssps7U8gX)
- **Development Help**: Open a GitHub discussion or issue
- **Feature Discussions**: Create a feature request issue first

Thank you for contributing to Phishing Club!
