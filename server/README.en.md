# Somnium Shade Cast Server

[Русский](README.md) · **English**

<p align="center">
  <img src="../assets/somnium-shade-cast-logo.png" alt="Somnium Shade Cast" width="600">
</p>

## Run the server with Docker

This setup requires Docker and Docker Compose. Commands run from the repository root. For a new library, the master key is generated once. Recovery requires a copy of `server/.env` with the original key, kept separately from the library files.

Key setup in Windows PowerShell.

```powershell
if (-not (Test-Path -LiteralPath 'server/.env')) {
    $sscKeyBytes = [byte[]]::new(32)
    $sscRng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $sscRng.GetBytes($sscKeyBytes) } finally { $sscRng.Dispose() }

    $sscTemplate = [IO.File]::ReadAllText((Resolve-Path -LiteralPath 'server/.env.example').Path)
    $sscEnv = $sscTemplate.Replace('SSC_MASTER_KEY=', 'SSC_MASTER_KEY=' + [Convert]::ToBase64String($sscKeyBytes))
    [IO.File]::WriteAllText((Join-Path (Get-Location) 'server/.env'), $sscEnv, [Text.UTF8Encoding]::new($false))
}
```

Key setup on Linux with OpenSSL installed.

```sh
if [ ! -e server/.env ]; then
  (
    umask 077
    ssc_key=$(openssl rand -base64 32) || exit 1
    sed "s|^SSC_MASTER_KEY=$|SSC_MASTER_KEY=$ssc_key|" server/.env.example > server/.env
  )
fi
```

Both variants create `server/.env` from the [template](.env.example) if it does not exist. For manual setup, `SSC_MASTER_KEY` is set to 32 bytes encoded in standard Base64. An existing library requires the original key used to encrypt its data. Older development builds used a built-in dev key even when the variable was set.

```sh
docker compose --env-file server/.env up --build --detach --wait
curl http://127.0.0.1:8080/health
```

In Windows PowerShell, the health check uses `curl.exe`. The server is published on the host loopback interface at `127.0.0.1:8080` by default. A different host port can be set via `SSC_PUBLISH_PORT` in `server/.env` and selected in the client. On a running server, the API reference is available at `http://127.0.0.1:8080/swagger/index.html`.

SQLite and encrypted audio are stored on the `server-data` volume and survive container replacement. The following commands show the logs and stop the server.

```sh
docker compose --env-file server/.env logs -f server
docker compose --env-file server/.env down
```

## Run the server locally

This setup requires the Go version in [go.mod](go.mod), `ffprobe` from FFmpeg, and `server/.env` with the key prepared as above. `ffprobe` must be available on `PATH` or at the path set in `SSC_FFPROBER_PATH`. The server starts from the `server` directory.

```sh
cd server
go run ./cmd/somnium-shade-cast
```

## Access modes

- `SSC_ENV=dev` is set in the template for local testing. Open routes allow creation and selection of test profiles. The server should remain on the loopback interface in this mode.
- `SSC_ENV=prod` enables HTTP Basic Auth for the administrator account. `SSC_ADMIN_NAME` and `SSC_ADMIN_PASSWORD` are set in `server/.env`. The account is created with `init-admin` while the server is stopped.

For Docker, the command runs from the repository root.

```sh
docker compose --env-file server/.env run --rm --build server init-admin
```

For a local setup, the command runs from the `server` directory.

```sh
go run ./cmd/somnium-shade-cast init-admin
```

After initialization, the server can start as usual. In `prod` mode, the client requires HTTPS for connections from another machine. Local HTTP connections are also supported.

## Configuration

Process environment takes precedence over the optional `.env` in the working directory, followed by an explicitly selected configuration file and built-in defaults. The YAML path is set with `--config ./config.yaml` or `SSC_CONFIG`. Relative paths resolve against the process working directory. In Docker, persistent data paths should be inside `/data`.

The master key is supplied through `SSC_MASTER_KEY`. Server settings can be overridden with the `SSC_` prefix. For example, `SSC_SERVER_PORT` maps to `server.port`, and `SSC_LOGS_LOG_FILE` sets the file log path.

An optional YAML config can use these fields.

```yaml
server:
  port: 8090
logs:
  log_file: ./data/logs/server.log
```
