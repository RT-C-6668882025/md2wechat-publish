# Tencent Cloud runtime

## Runtime requirements

- Linux amd64 or arm64
- CA certificates and DNS resolution
- Read access to the job directory and write access to output paths
- Outbound HTTPS access; no inbound port is required by the CLI
- A stable public egress IP or fixed proxy for WeChat IP allowlisting

Add the instance EIP or NAT gateway egress IP to the WeChat Official Account allowlist. If the instance has unstable egress, configure `WECHAT_PROXY_URL` with a fixed-egress HTTP/HTTPS proxy.

## Environment

Provide secrets through the orchestrator, systemd `EnvironmentFile`, Docker secrets, or a permission-restricted environment file:

```bash
export MD2WECHAT_API_KEY="..."
export MD2WECHAT_BASE_URL="https://your-convert-api.example.com"
export WECHAT_APPID="..."
export WECHAT_SECRET="..."
export HTTP_TIMEOUT="60"
```

Optional values include `WECHAT_ACCOUNT`, `WECHAT_PROXY_URL`, `DEFAULT_THEME`, and `DEFAULT_BACKGROUND_TYPE`.

Do not configure image or text-model credentials for this publishing skill. Those belong to the upstream generation system.

Alternatively, pass `--config /secure/path/config.yaml`. Environment variables override file values. Restrict secret-bearing files to the runtime user, for example mode `0600`.

## Scheduling

Run one process per publishing job from cron, systemd, a queue worker, or a container task. Use a unique immutable directory per job. Capture stdout as the machine result and use the process exit code as the scheduler result.

Never schedule blind retries for `draft`. On timeouts or connection resets after the request was sent, reconcile WeChat state before retrying.

## Container build

Build directly from the standalone publishing project:

```bash
cd projects/md2wechat-publish
docker build -f deploy/Dockerfile \
  --build-arg VERSION=2.9.0 \
  -t md2wechat-publish:2.9.0 .
```

Mount each prepared job read-only and write results to a separate writable directory. Pass credentials at runtime, never during `docker build`.
