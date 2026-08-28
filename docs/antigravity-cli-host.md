# Antigravity CLI Host

`antigravity_cli_host` runs `agy` on the Linux host instead of inside the
GoClaw container. It replaces the retired `antigravity-runtime` sidecar.

## Install

1. Install the `goclaw` host bridge binary at `/usr/local/bin/goclaw-agy-host` and AGY at
   `/root/.local/bin/agy`.
2. Generate a bridge token and save the following file with mode `0600`:

```sh
install -d -m 700 /etc/goclaw
cat >/etc/goclaw/agy-host.env <<'EOF'
GOCLAW_AGY_HOST_TOKEN=replace-with-a-long-random-secret
EOF
chmod 600 /etc/goclaw/agy-host.env
```

3. Copy `deploy/goclaw-agy-host.service` to
   `/etc/systemd/system/goclaw-agy-host.service`, then run:

```sh
systemctl daemon-reload
systemctl enable --now goclaw-agy-host
```

4. Add these values to GoClaw's `.env`, then recreate the GoClaw container:

```sh
GOCLAW_AGY_HOST_BRIDGE_URL=http://host.docker.internal:18891
GOCLAW_AGY_HOST_TOKEN=the-same-long-random-secret
docker compose up -d --force-recreate goclaw
```

## Provider login

Create an **Antigravity CLI (Host)** provider. Open the provider detail page
and choose **Open AGY terminal**. Complete the interactive Google login in the
embedded terminal, then use AGY's `/model` command to select the desired model.
Each provider name has a separate AGY profile under
`/var/lib/goclaw-agy/profiles/<provider-name>`.

The included systemd unit binds to Docker's host-gateway address
`172.17.0.1:18891`; do not publish this port through Docker or expose the
token in a provider API key.
