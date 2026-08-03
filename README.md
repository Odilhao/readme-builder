# readme-builder

A credential-free Go tool that renders a README (or any file) from public GitHub activity and RSS feeds. No token required — the tool works with public data and needs only unauthenticated GitHub API access (60 requests/hour). A token may be supplied to raise the rate limit to 1000/hour, but it is never required.

This is a clean-room implementation that replaces the unmaintained `muesli/markscribe` and `muesli/readme-scribe` pair.

## Quick start

### Binary

Build and run the binary locally:

```bash
go build -o readme-builder ./cmd/readme-builder

./readme-builder -config config.toml
```

Flags:
- `-config <path>`: **Required**. Path to a TOML configuration file.
- `-write <path>`: Write rendered output to this path (`-` for stdout). If omitted, uses paths in config.
- `-check`: Render and exit with status 1 if the output would drift from existing files (useful in CI).
- `-dump-json <path>`: Emit the resolved data model as JSON to this path (`-` for stdout), without rendering templates.
- `-v`: Verbose output to stderr.
- `-version`: Print version and exit.

### Podman container

Build and run via Podman:

```bash
podman build -t readme-builder .
podman run --rm \
  -v "$PWD:$PWD:z" \
  -w "$PWD" \
  readme-builder \
  -config config.toml
```

The image ships as `ghcr.io/odilhao/readme-builder:latest` and as tagged releases like `v1.2.3`.

### GitHub Action (composite)

Use as a GitHub Action:

```yaml
- name: Render README
  uses: Odilhao/readme-builder@v1  # or a specific commit SHA: @abc123def456
  with:
    config: config.toml       # Path to TOML config, relative to repo root
    check: false              # Set to true to validate drift only
    version: false            # Set to true to print version and exit
```

The action runs the published OCI image using Podman on the GitHub runner. It does not require any credential.

## Permissions and the no-credential guarantee

This tool works entirely with public data and requires no credential. A GitHub token can optionally be supplied to raise the unauthenticated rate limit from 60 to 1000 requests per hour, but the tool functions correctly without it.

### In a workflow: the split-job pattern

Never give the job that runs readme-builder a write token. Use the **split-job pattern**:

1. **Data job** (no token write permission):
   - Checks out the repository.
   - Runs readme-builder without any credential.
   - Commits rendered output via a credential-free mechanism (e.g., a force push from a bot account, or a pull request via GitHub Actions' built-in mechanism).

2. **Gate job** (write token):
   - Runs automated checks, linting, or other verification.
   - Has access to `GITHUB_TOKEN` to commit back to main or merge a PR.

This separation ensures that even if the data-fetching step is compromised, an attacker cannot write directly to the repository — they can only propose changes that pass the gate.

### Workflow example

```yaml
jobs:
  fetch-and-render:
    permissions:
      contents: read  # Only read the config file
    runs-on: ubuntu-latest
    steps:
      - name: Check out
        uses: actions/checkout@v7
        with:
          persist-credentials: false
      
      - name: Render
        uses: Odilhao/readme-builder@v1
        with:
          config: config.toml
      
      - name: Upload rendered output
        uses: actions/upload-artifact@v4
        with:
          name: rendered-output
          path: README.md

  gate:
    needs: fetch-and-render
    permissions:
      contents: write
    runs-on: ubuntu-latest
    steps:
      - name: Check out
        uses: actions/checkout@v7
      
      - name: Download rendered output
        uses: actions/download-artifact@v4
        with:
          name: rendered-output
      
      - name: Commit if changed
        run: |
          git config user.name "bot"
          git config user.email "bot@example.com"
          git add README.md
          git commit -m "chore: render README" || echo "No changes"
          git push
```

## Configuration

Create a `config.toml` file to specify data sources and output targets:

```toml
username = "octocat"

[github]
exclude_forks = true                              # Optional; default false
exclude_repos = ["example-org/example-repo"]      # Optional
exclude_orgs = ["example-org"]                    # Optional

[github.contributions]
limit = 10  # Limit output to 10 most recent; omit for default (10)

[github.repos]
# Present but empty: fetch with default limit

[github.pull_requests]
limit = 5

# Available sections: contributions, repos, forks, pull_requests, stars, releases, followers, gists
# All sections are optional. An absent section yields no data.

[feeds.blog]
url = "https://example.com/feed.xml"
limit = 3   # Omit for default (5)

[feeds.news]
url = "https://example.com/news.xml"

[[render]]
template = "templates/README.md.tmpl"
output = "README.md"

[[render]]
template = "templates/index.html.tmpl"
output = "docs/index.html"
```

### Configuration rules

- `username`: **Required**. The GitHub user or organization to fetch data for (e.g., `octocat`).
- `github`: **Optional**. Subsection configuring GitHub data sources and filters.
  - `exclude_forks`: Boolean; default false.
  - `exclude_repos`: Array of `owner/repo` strings to exclude.
  - `exclude_orgs`: Array of organization names to exclude by owner.
- GitHub sections: `contributions`, `repos`, `forks`, `pull_requests`.
  - Additional sections (`stars`, `releases`, `followers`, `gists`) are defined in the model but not yet implemented; configuring them has no effect.
  - Each section is optional. If omitted, no data is fetched for it.
  - Each section accepts an optional `limit` (integer ≥ 0). Default is 10.
  - A `limit` of 0 explicitly yields no items for that section.
- `feeds`: **Optional**. Map of feed name to configuration.
  - Feed names must match `[A-Za-z_][A-Za-z0-9_]*` (valid in Go templates as `.Feeds.name`).
  - Each feed requires a `url` and optional `limit` (default 5).
- `render`: **Required**. Array of template → output pairs.
  - At least one `[[render]]` section is required.
  - `template`: Path to a Go text template file.
  - `output`: Path to write the rendered output.
  - Paths can be absolute or relative to the config file's directory.

## SHA-pinning guidance

Always pin Actions to a commit SHA, never a floating tag. This prevents an upstream repository takeover from injecting malicious code into your CI:

**Good:**
```yaml
uses: Odilhao/readme-builder@abc123def456  # v1.2.3
```

**Bad:**
```yaml
uses: Odilhao/readme-builder@v1  # Could change any time
```

To find a commit SHA for a release tag, use:

```bash
gh api repos/Odilhao/readme-builder/git/refs/tags/v1.2.3 --jq '.object.sha'
```

Or check the GitHub repository page: releases are listed with their commit SHAs.

## Provenance and verification

Releases are published with SLSA provenance attestations. Verify them:

```bash
# Image provenance
gh attestation verify oci://ghcr.io/odilhao/readme-builder:v1.2.3 \
  --repo Odilhao/readme-builder

# Binary provenance
gh attestation verify readme-builder_linux_amd64.tar.gz \
  --repo Odilhao/readme-builder

# SBOM provenance
gh attestation verify --predicate-type cyclonedx \
  oci://ghcr.io/odilhao/readme-builder:v1.2.3 \
  --repo Odilhao/readme-builder
```

Release artifacts are listed in GitHub Releases and include:
- Binary tarballs (linux/amd64 and linux/arm64)
- Checksums (SHA256)
- Source tarball
- OCI image digest (immutable multi-arch image reference)
- Attestations for all of the above

## Known limits

### API budget

The GitHub REST API provides:
- **Unauthenticated: 60 requests/hour** (per IP or user agent)
- **Authenticated: 1000 requests/hour** (with a valid token)

This tool starts every run without a token, making at most one request per configured section plus one per repository checked for fork status. Plan accordingly if using many sections or exclusion rules.

### Public events API

Contributions are derived from `GET /users/{username}/events/public`, which has these constraints:
- **Public timeline only:** Private events, private contributions, and pull requests to private repositories are not visible.
- **~90-day retention:** The API returns only recent public events; older activity is not available.
- **~300-event ceiling:** The API returns at most 300 events total across all repositories, paginated as 100 items per page (3 pages max).

This is a floor, not a ceiling — the `Events` count in contributions reflects only what the API returned, not a lifetime total.

### Rate limiting under these constraints

A typical run with all sections enabled and moderate configuration (e.g., `limit: 10` per section) uses approximately 4–6 requests:
- 1 for public events (contributions)
- 1 for user repos
- 1 for starred repos
- 1 for pull requests
- 1–2 for fork status checks (if `exclude_forks: true`)
- 1 per RSS feed (if configured)

This leaves ample headroom within the unauthenticated budget.

## Development

### Testing

```bash
go test ./... -race -count=1
```

Run tests without a GitHub token to verify the no-credential guarantee:

```bash
env -u GITHUB_TOKEN -u GH_TOKEN go test ./... -count=1
```

### Building

```bash
go build -o readme-builder ./cmd/readme-builder
```

For reproducible builds with source-date epoch:

```bash
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct) goreleaser build --snapshot
```

### Hermetic builds

This repository uses hermeto to prefetch Go dependencies and build offline:

```bash
podman run --rm \
  -v "$PWD:$PWD:z" \
  -w "$PWD" \
  ghcr.io/hermetoproject/hermeto:latest \
  prefetch ./go.mod -o ./hermeto-output

podman build \
  --volume ./hermeto-output:/tmp/hermeto-output:Z \
  --volume ./hermeto.env:/tmp/hermeto.env:Z \
  --network=none \
  .
```

The CI workflow automates this. Manual builds without hermeto will fail at the container build stage.

## License

GNU General Public License v3.0 or later. See `LICENSE` for details.

## Prior art

This tool is a clean-room implementation inspired by and replacing:
- [muesli/markscribe](https://github.com/muesli/markscribe)
- [muesli/readme-scribe](https://github.com/muesli/readme-scribe)

Both projects are no longer maintained. We are grateful for the ideas they pioneered.
