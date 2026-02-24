# Claude Code Testing Sandbox

A reusable Docker sandbox for testing Claude Code configurations.

## Quick Start

### First Time Setup

```bash
# Build image
docker build -t claude-sandbox -f tests/Dockerfile.sandbox .

# Create persistent container
docker run -it --name sandbox claude-sandbox bash

# Authenticate (inside container)
claude  # Follow OAuth prompts, then exit
```

### Future Sessions

```bash
# Start sandbox
docker start sandbox && docker exec -it sandbox bash

# Copy any project in
docker cp ~/Github/myproject/. sandbox:/workspace/myproject

# Stop when done
docker stop sandbox
```

## What's Installed

- Ubuntu 22.04
- Node.js 20
- Claude Code CLI
- dot-agents CLI
- git, jq, vim

## Run Tests

```bash
# Inside sandbox
cd /workspace
bash test-claude-configs.sh
```

## Re-authenticate

If auth expires, run inside sandbox:

```bash
claude  # or /login
```

## Cleanup

```bash
docker rm sandbox           # Delete container
docker rmi claude-sandbox   # Delete image
```

## Quick Reference

| Action | Command |
|--------|---------|
| Start sandbox | `docker start sandbox && docker exec -it sandbox bash` |
| Stop sandbox | `docker stop sandbox` |
| Copy project in | `docker cp ~/path/to/project/. sandbox:/workspace/name` |
| Re-authenticate | Inside sandbox: `claude` or `/login` |
| Check status | `docker ps -a \| grep sandbox` |
| Delete sandbox | `docker rm sandbox` |
| Rebuild image | `docker build -t claude-sandbox -f tests/Dockerfile.sandbox .` |
