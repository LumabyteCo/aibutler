# Docker Deployment

## Quick Start

Build and run AI Butler with Docker:

```bash
docker compose up -d
```

This starts AI Butler on ports 8080 (web) and 8081 (A2A).

## With Ollama (Local LLM)

```bash
docker compose -f docker-compose.ollama.yml up -d
```

## Full Stack (Ollama + Home Assistant)

```bash
docker compose -f docker-compose.full.yml up -d
```

## Build Only

```bash
docker build -t aibutler .
docker run -d -p 8080:8080 -v aibutler-data:/data aibutler
```

## Health Check

The container includes a built-in health check at `/healthz`:

```bash
curl http://localhost:8080/healthz
```

## Data Persistence

All data is stored in the `/data` volume. Mount a named volume or host path to persist across container restarts.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AIBUTLER_DATA` | Data directory path | `/data` |
