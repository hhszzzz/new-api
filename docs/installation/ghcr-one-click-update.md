# GHCR one-click updates

The system-maintenance page checks the latest successful
`publish-fork-image.yml` run for `hhszzzz/new-api`. A completed run represents
an image that has already been published as `ghcr.io/hhszzzz/new-api:main`.

The application intentionally does not receive direct access to the Docker
socket. One-click updates call an authenticated, deployment-local update
trigger instead. Watchtower's HTTP API is one supported trigger.

## Docker Compose example

Add the following values to the application service. Use the same private
Compose network for the application and updater services.

```yaml
services:
  new-api:
    image: ghcr.io/hhszzzz/new-api:main
    pull_policy: always
    environment:
      SYSTEM_UPDATE_TRIGGER_URL: "http://new-api-updater:8080/v1/update?image=ghcr.io/hhszzzz/new-api:main&async=true"
      SYSTEM_UPDATE_TRIGGER_TOKEN: ${SYSTEM_UPDATE_TRIGGER_TOKEN:?set SYSTEM_UPDATE_TRIGGER_TOKEN}
    labels:
      com.centurylinklabs.watchtower.enable: "true"
    networks:
      - new-api-network

  new-api-updater:
    image: nickfedor/watchtower:1.20.2
    restart: unless-stopped
    command: --http-api-endpoints update --label-enable --cleanup
    environment:
      WATCHTOWER_HTTP_API_TOKEN: ${SYSTEM_UPDATE_TRIGGER_TOKEN:?set SYSTEM_UPDATE_TRIGGER_TOKEN}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    labels:
      com.centurylinklabs.watchtower.enable: "false"
    networks:
      - new-api-network
```

Generate a long random `SYSTEM_UPDATE_TRIGGER_TOKEN` in the deployment `.env`.
Do not publish the updater port to the host or public network. The application
calls it only over the private Compose network.

If the deployment uses a different service name or network, keep the URL and
network membership consistent. The update recreates only the labeled
application container; PostgreSQL, MySQL, SQLite data, Redis, and mounted data
directories remain unchanged.

Optional source overrides are available through
`SYSTEM_UPDATE_REPOSITORY`, `SYSTEM_UPDATE_WORKFLOW`,
`SYSTEM_UPDATE_BRANCH`, and `SYSTEM_UPDATE_IMAGE`. Public repositories do not
need `SYSTEM_UPDATE_GITHUB_TOKEN`; it can be set to increase GitHub API limits
or check a private repository.
