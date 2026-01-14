# GitHub Actions Workflows

This directory contains GitHub Actions workflows for Beacon.

## Workflows

### build.yml - Continuous Integration

**Triggers:**
- Push to `main` or `develop` branches
- Pull requests to `main` or `develop` branches

**What it does:**
- Tests with multiple Go versions (1.21, 1.22)
- Checks code formatting with `gofmt`
- Runs `go vet` static analysis
- Builds the project
- Runs `golangci-lint` with all linters

**Status badge for README:**
```markdown
[![Build](https://github.com/SimonLariz/beacon/actions/workflows/build.yml/badge.svg)](https://github.com/SimonLariz/beacon/actions/workflows/build.yml)
```

### release.yml - Automated Releases

**Triggers:**
- Push a tag matching the pattern `v*` (e.g., `v1.0.0`, `v1.2.3`)

**What it does:**
- Builds binaries for multiple platforms:
  - Linux (amd64, arm64)
  - macOS (amd64, arm64)
  - Windows (amd64)
- Creates a GitHub Release with:
  - All compiled binaries as attachments
  - Auto-generated release notes from commits

**How to create a release:**

1. Commit all changes and push to main
2. Create an annotated tag:
   ```bash
   git tag -a v1.0.0 -m "Release version 1.0.0"
   git push origin v1.0.0
   ```
3. GitHub Actions will automatically:
   - Build binaries for all platforms
   - Create a release on GitHub
   - Attach the binaries to the release

**Example releases:**
- `v1.0.0` - Release 1.0.0
- `v1.2.3-beta` - Beta release
- `v2.0.0-rc1` - Release candidate

## Troubleshooting

### Workflow not running
- Check that the workflow files are in `.github/workflows/`
- Verify the trigger conditions (branch names, tag patterns)
- Check Actions tab on GitHub to see logs

### Build failures
- Review the workflow logs on GitHub (Actions tab)
- Common issues:
  - Go version incompatibility
  - Formatting issues (run `go fmt ./...` locally)
  - Linting errors (run `golangci-lint run ./...` locally)

### Release workflow issues
- Ensure tag follows the pattern `v*` (e.g., `v1.0.0`)
- Check that all commits are pushed before creating the tag
- Verify the tag is pushed to origin: `git push origin <tag-name>`

## Customization

To modify workflows:

1. Edit `.github/workflows/build.yml` or `.github/workflows/release.yml`
2. Commit and push changes
3. New workflow behavior takes effect on next trigger

Common customizations:
- Add more Go versions to test matrix
- Add more platforms to release builds
- Add additional linting rules
- Add tests to the build workflow
- Add code coverage reporting
