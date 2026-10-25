# Docker

The Docker tool of [Web IDE](https://github.com/antennae-space/web-ide): the Compose stack of a project, the containers of the host, their logs and statistics, the disk Docker uses, a shell in a container, and tools for the AI. An official plugin: the IDE installs it by default.

## What it does

- **Project**: the services of the Compose file at the root of the project (`compose.yaml`, `docker-compose.yml`…), with the profiles you choose; start, stop, restart the stack or a service; recreate, rebuild, pull, down, down with volumes (the name typed to confirm) in a console of the IDE that shows their progress.
- **Host**: every container, grouped by Compose project; start, stop, restart, remove.
- **Details**: image, state, command, ports, mounts, networks, CPU and memory; logs followed live, with a filter, a service filter for the stack, timestamps and ANSI colors.
- **Shell**: bash (or sh) in a running container, as its user or root, in a console of the IDE.
- **Disk**: the space of images, containers, volumes and the build cache; prune them (each asked first), remove an unused image or volume.
- **For the AI** (the assistant, Claude Code through the MCP, the agents): `docker_ps` (the state of the services) and `docker_logs` (the last lines of a service or a container), read only, with the profiles chosen in the tool.

It reaches Docker only through the socket the IDE opens for it: the Engine API, and Compose as a Go library (`github.com/docker/compose/v5`), not the `docker` command, which the host need not have. The containers carry the labels of `docker compose`: the command and the tool see the same stack.

Not yet: private registries (the plugin reads neither `~/.docker/config.json` nor a credential helper). On a project over SSH, the Docker of its host, and a Compose file that includes or extends another file is refused.

## Permissions

| | |
|---|---|
| `socket:docker` | the Docker socket of the machine of the project, opened by the IDE: **as much as root** on that machine |
| `terminal` | consoles of the IDE it feeds: the shells, the long Compose actions |
| `files:read` | the Compose file of a local project and the files it names |
| `core:fs.read` | the Compose file of a project over SSH, read through the IDE |

## Develop

Built on the [plugin SDK](https://github.com/antennae-space/plugin-sdk). `npm ci && npm run build` (the page module), `go test ./...` (the backend; with a local Docker and the `busybox` image, a stack is started and removed), `./scripts/pack.sh` (the archives). A tag `v*` publishes a release.

## License

MIT.
