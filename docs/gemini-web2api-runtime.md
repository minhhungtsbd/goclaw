# Gemini Web2API Runtime

GoClaw can optionally bundle `Sophomoresty/gemini-web2api` and run it as a
loopback-only process in the same container. This avoids a second Docker image
and service while keeping the provider compatible with GoClaw's standard
OpenAI request path.

The upstream source is pinned to commit
`2bb988bfcbb82a7fab5d2c99aa5560ff40d64f7e` and verified with BuildKit
checksums. Its MIT license is copied into the image with the runtime.

## Enable

Set this in `.env`, then rebuild GoClaw:

```env
ENABLE_GEMINI_WEB2API=true
```

```bash
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.browser.yml build goclaw
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.browser.yml up -d --no-deps goclaw
```

The runtime listens on `127.0.0.1:8081` inside the GoClaw container and is not
published to the host. Startup failure is non-fatal: GoClaw continues without
this fallback and writes a warning to the container log.

## Configure the provider

In **Providers**, create **Gemini Web2API (Local)**. The form pre-fills:

- Alias: `gemini-web2api`
- API base: `http://127.0.0.1:8081/v1`
- API key: not required

Select a model returned by `/v1/models`, then add this provider at the end of
an agent's model fallback list.

## Safety and limitations

Gemini Web2API uses a reverse-engineered Gemini Web endpoint rather than the
official Google API. The upstream protocol, anonymous access, model mapping,
rate limits, or image upload flow can change without notice. Use it as the last
fallback route, not as the primary provider.

GoClaw validates fallback capabilities and response structure before accepting
a result. A route is skipped when it cannot satisfy required tools or visual
input, returns an empty response, calls an unavailable tool, or ignores a
required tool choice.

For authenticated Gemini Web access, place a custom config and cookie under
`/app/data`, set `GOCLAW_GEMINI_WEB2API_CONFIG` to that config path, and keep
the runtime bound to `127.0.0.1`.
