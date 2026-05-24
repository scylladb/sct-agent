# SCT Agent - MVP Implementation

A lightweight Go-based HTTP API service designed to replace SSH-based command execution in the Scylla Cluster Tests (SCT) framework.

This is an MVP implementation, which provides core functionality:

- **Core HTTP API Server** with Gin framework
- **RESTful Command Execution** API (POST /api/v1/commands)
- **Job Status Monitoring** (GET /api/v1/commands/{id})  
- **Job Listing & Filtering** (GET /api/v1/commands)
- **Command Cancellation** (DELETE /api/v1/commands/{id})
- **Health Check Endpoint** (/health)
- **API Key Authentication** with Bearer token support
- **Concurrent Job Execution** with configurable concurrency limits
- **In-Memory Job Storage** with proper cleanup policies
- **Go Client Library** for easy integration
- **Configuration Management** via YAML files

## Quick Start

### 1. Install or build the agent

Download the latest release for your architecture:

```bash
ARCH=$(uname -m); case "$ARCH" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
esac
curl -fsSL -o sct-agent \
  "https://github.com/scylladb/sct-agent/releases/latest/download/sct-agent-linux-${ARCH}"
chmod +x sct-agent
```

Or build from source:

```bash
make build && cp bin/sct-agent .
```

Verify the binary:

```bash
./sct-agent --version
```

### 2. Start the Agent

```bash
./sct-agent --config configs/agent.yaml
```

The agent will start on `http://localhost:16000` by default.

### 3. Test with curl

```bash
# execute a command
curl -X POST http://localhost:16000/api/v1/commands \
  -H "Authorization: Bearer sct-runner-key-1" \
  -H "Content-Type: application/json" \
  -d '{"command": "echo", "args": ["Hello", "World"]}'

# check job status
curl -H "Authorization: Bearer sct-runner-key-1" \
  http://localhost:16000/api/v1/commands/{JOB_ID}
```

## Configuration

### Configuration File

Edit `configs/agent.yaml` to customize:

```yaml
server:
  host: "0.0.0.0"
  port: 16000

security:
  api_keys:
    - "sct-runner-key-1"
    - "sct-runner-key-2"

executor:
  max_concurrent_jobs: 10
  default_timeout_seconds: 1800
```

### Environment Variables

API key can be also provided via environment variable:

```bash
export SCT_AGENT_API_KEY="secure-api-key"
./sct-agent --config configs/agent.yaml
```

**Note:** API keys from environment variables are added to the list of valid keys from the configuration file.

## API Endpoints

- `GET /health` - Health check (no auth)
- `POST /api/v1/commands` - Execute command
- `GET /api/v1/commands/{id}` - Get job status  
- `GET /api/v1/commands` - List jobs (with filtering)
- `DELETE /api/v1/commands/{id}` - Cancel job

## Maintainer guide

### Cut a release

Releases are produced automatically by the **Release** workflow when a `vX.Y.Z` tag is pushed to a commit that is reachable from `master`.

```bash
git checkout master
git pull
git tag v0.0.3
git push origin v0.0.3
```

The workflow validates the tag, runs the full CI suite, cross-compiles both Linux architectures, and publishes a GitHub Release with auto-generated notes.

To retry a release after a transient failure (e.g. a flaky upload), re-run the **Release** workflow from the Actions tab against the same tag — no need to re-tag.

### Cut a dev (prerelease) build

Trigger the **Dev Release** workflow from the Actions tab. Optionally set `next_version` to the version line this dev build targets (e.g. `0.1.0`); if omitted, the workflow patch-bumps the last non-dev tag.

The resulting tag has the form `v<next>-dev.YYYYMMDD.<shortsha>` and is marked as a prerelease. Dev prereleases older than 15 days are pruned automatically on each dev-release run.
