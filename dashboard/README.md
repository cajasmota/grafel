# Archigraph Dashboard

The dashboard is a React/Vite SPA that visualises indexed graph data served by
`archigraph dashboard serve`. It can run against live daemon data or against
bundled mock JSON for UI development.

## Running with real data

Follow these steps to view actual indexed graph data in the dashboard.

### 1. Build the CLI

```sh
go build -o archigraph ./cmd/archigraph
```

### 2. Register and index a repository

```sh
# Create a group (one-time)
./archigraph register --group my-group /path/to/your/repo

# Index it (re-run after code changes)
./archigraph index /path/to/your/repo
```

For a quick local fixture use the bundled golden corpus:

```sh
./archigraph register --group golden internal/quality/golden/go-chi-mini
./archigraph index internal/quality/golden/go-chi-mini
```

### 3. Start the dashboard server on a fixed port

Pin the server to port 31000 so the Vite proxy always knows where to find it:

```sh
./archigraph dashboard serve --port 31000
# Output: 31000
# Stderr: archigraph dashboard listening on http://127.0.0.1:31000/
```

### 4. Start the Vite dev server

In a separate terminal, from the `dashboard/` directory:

```sh
# Copy the example env if you haven't already
cp .env.example .env

# VITE_USE_MOCKS must be absent or 'false'; VITE_API_PORT must match --port above
VITE_API_PORT=31000 VITE_USE_MOCKS=false npm run dev
```

Open <http://127.0.0.1:5173> — all five surfaces (Graph, Flows, Topology, Paths,
Docs) now render live data from the group you registered.

### 5. Switching between mock and real mode

| Mode | Command |
|------|---------|
| Real data (default) | `VITE_USE_MOCKS=false npm run dev` |
| Mock data (UI development) | `VITE_USE_MOCKS=true npm run dev` |

No rebuild is needed when switching modes — just restart the dev server.

## Running tests

```sh
npm test
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `VITE_USE_MOCKS` | `false` | Set to `true` to use bundled mock JSON |
| `VITE_API_PORT` | `31000` | Port the dashboard server is bound to |
