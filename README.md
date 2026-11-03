# DevPod Provider Hetzner

<!-- markdownlint-disable-next-line MD013 MD034 -->
[![Go Report Card](https://goreportcard.com/badge/github.com/FKint/devpod-provider-hetzner)](https://goreportcard.com/report/github.com/FKint/devpod-provider-hetzner)

DevPod on Hetzner

This is a fork of
[mrsimonemms/devpod-provider-hetzner](https://github.com/mrsimonemms/devpod-provider-hetzner),
created by Simon Emms. All credit for the original work goes to the original author.

<!-- toc -->
- [Project status](#project-status)
- [Usage](#usage)
  - [Persistent storage](#persistent-storage)
    - [Machine options](#machine-options)
    - [Per-workspace settings](#per-workspace-settings)
    - [Recommended devcontainer settings](#recommended-devcontainer-settings)
- [Development](#development)
  - [Required environment variables](#required-environment-variables)
  - [Testing independently of DevPod](#testing-independently-of-devpod)
  - [Testing in the DevPod ecosystem](#testing-in-the-devpod-ecosystem)
  - [Releasing](#releasing)
- [Contributing](#contributing)
  - [Open in a container](#open-in-a-container)
  - [Commit style](#commit-style)
<!-- /toc -->

> Use [this referral code](https://hetzner.cloud/?ref=UWVUhEZNkm6p) to get €20 in
> credits (at time of writing).

[DevPod](https://devpod.sh/) on [Hetzner](https://hetzner.cloud/?ref=UWVUhEZNkm6p).
This is based upon the [DigitalOcean provider](https://github.com/loft-sh/devpod-provider-digitalocean).

## Project status

**This project is feature-complete and in maintenance-mode**.

This project is very much alive and will receive regular dependency updates. I
do not intend to add any new features unless requested by a large number of people
or to fix changes made by DevPod or Hetzner.

## Usage

To use this provider in your DevPod setup, you will need to do the following steps:

1. See the [DevPod documentation](https://devpod.sh/docs/managing-providers/add-provider)
   for how to add a provider
1. Use the reference `FKint/devpod-provider-hetzner` to download the latest
   release from GitHub
1. Get an [API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/)
   from Hetzner with `read & write` access. This will be used to manage resources.

### Persistent storage

`stop` deletes the server; only its Hetzner Cloud volume (sized by
`DISK_SIZE`) survives until `delete`. Each DevPod machine has one volume.
Workspaces sharing a machine (`devpod up --machine ...`, or a provider set up
with `--single-machine`) share it. It holds:

- the `devpod` user's home directory, including the workspace sources;
- by default (`PERSIST_DOCKER`) all of Docker's state: its data root
  (`/home/devpod/.devpod/docker`: named volumes, container configs, build cache)
  and containerd's root (`/home/devpod/.devpod/containerd`: images and container
  filesystems).

By default, space is kept in check:

- the build cache is capped at 5 GB;
- container logs use the rotating `local` log driver;
- at every start, before DevPod connects, images not used by any container and
  older than a week are removed, as are unused anonymous volumes. Images used by
  any container (running or stopped, of any workspace) and all named volumes
  are kept.

Plan for roughly 25–30 GB per workspace: two copies of the dev container image
during a rebuild, its named volumes and sources, plus 20% headroom. Add about
10 GB for every further similar workspace. `delete` removes the volume, and with
it the data of **every** workspace on the machine.

Persisted Docker state can be upgraded but not downgraded. Don't switch
`DISK_IMAGE` to an image with an older Docker on an existing machine.

#### Machine options

These provider options set up the server itself, so they apply to every
workspace on the machine. They take effect whenever the server is created, i.e.
on the next start after a stop. Set defaults with
`devpod provider set-options hetzner -o KEY=VALUE`, or change them for one
workspace with `devpod up <workspace> --provider-option KEY=VALUE`.

| Option | Default | Effect |
| --- | --- | --- |
| `DISK_SIZE` | `30` | Size of the volume in GB, when it is created. |
| `PERSIST_DOCKER` | `true` | Keep Docker's images, containers, named volumes and build cache on the volume. With `false` they live on the server's local disk (faster, but lost on every stop). |
| `CONTAINERD_IMAGE_STORE` | `true` | Use Docker's containerd image store. Switching it on an existing machine hides the other store's images and containers; they are rebuilt on the next start. |
| `BUILD_CACHE_SIZE` | `5GB` | Build cache size kept by garbage collection. |
| `PRUNE_IMAGES_AFTER` | `168h` | At start, remove images not used by any container that are older than this. `never` keeps them. |
| `PRUNE_ANONYMOUS_VOLUMES` | `true` | At start, remove anonymous volumes not used by any container. Named volumes are never removed. |
| `SWAP_SIZE` | `8` | Swap file in GB on the local disk (also where a tmpfs `/tmp` spills to). `0` disables it. |
| `CONTAINERS_MEMORY_HIGH` | `infinity` | All containers together are throttled above this: a percentage of RAM, a size (`6G`) or `infinity`. Left off on purpose: a container throttled below the hard limit, with swap full, hung for minutes instead of being killed. |
| `CONTAINERS_MEMORY_MAX` | `90%` | Hard limit for all containers together; above it the OOM killer stops a container process (within seconds in testing). |
| `CONTAINERS_SWAP_MAX` | `4G` | Swap all containers together may use: a size or `infinity`. Keeps swap for the host. |

#### Per-workspace settings

Everything that concerns a single dev container is set in its
`devcontainer.json`, not in the provider:

| To | Set in `devcontainer.json` |
| --- | --- |
| Keep `/tmp` off the volume (in RAM, spilling to local swap) | `"mounts": ["type=tmpfs,target=/tmp,tmpfs-mode=1777,tmpfs-size=4g"]` |
| …and allow running programs from it | `"runArgs": ["--tmpfs", "/tmp:exec,mode=1777,size=4g"]` instead (Docker mounts tmpfs `noexec` by default) |
| Keep `/tmp` on disk | Leave it out: `/tmp` is then part of the container's filesystem, on the volume with `PERSIST_DOCKER` |
| Enlarge shared memory (browsers, PyTorch data loaders) | `"runArgs": ["--shm-size=2g"]` |
| Limit one container's memory or CPUs | `"runArgs": ["--memory=6g", "--cpus=2"]` (within `CONTAINERS_MEMORY_MAX`) |
| Change log rotation | `"runArgs": ["--log-opt", "max-size=50m", "--log-opt", "max-file=3"]` |
| Use another log driver | `"runArgs": ["--log-driver=json-file"]` |
| Persist a directory per workspace | `"mounts": ["source=my-project-${devcontainerId}-data,target=/data,type=volume"]` |
| Share a cache between workspaces | A named volume with a fixed name, e.g. `source=uv-cache,...` |
| Scratch space on the local disk (fast, lost on stop, shared by the machine's workspaces) | `"mounts": ["source=/var/tmp,target=/scratch,type=bind"]` |
| Set environment variables, e.g. cache locations | `"containerEnv": {"UV_CACHE_DIR": "/home/vscode/.uv-cache"}` |

`devpod up --recreate` and `.devpodignore` (below) are per workspace too.

#### Recommended devcontainer settings

Start with `devpod up --recreate` to get a fresh container every time. The
image is kept on the volume, so this skips the build unless the configuration
changed.

```jsonc
{
  "mounts": [
    // /tmp in RAM, spilling to swap on the local disk, never to the volume.
    // Keep the sizes of all workspaces on a machine within RAM + swap.
    "type=tmpfs,target=/tmp,tmpfs-mode=1777,tmpfs-size=4g",
    // Per-workspace state: include ${devcontainerId}, otherwise workspaces
    // on the same machine share the volume.
    "source=my-project-${devcontainerId}-state,target=/home/vscode/.state,type=volume",
    // Keeps the VS Code server and extensions across container recreation
    "source=my-project-${devcontainerId}-vscode,target=/home/vscode/.vscode-server,type=volume",
    // Package caches: deliberately one per machine, shared by its workspaces
    "source=uv-cache,target=/home/vscode/.uv-cache,type=volume"
  ],
  "containerEnv": { "UV_CACHE_DIR": "/home/vscode/.uv-cache" }
}
```

- Named volumes mounted at paths that don't exist in the image are owned by
  root, and so are any parent directories Docker has to create (so avoid e.g.
  `~/.cache/uv` if `~/.cache` isn't in the image). Fix the top-level directory
  once (`sudo chown vscode: <dir>`). Avoid `chown -R`, which re-walks the whole
  volume on every container creation.
- Keep `onCreateCommand`/`postCreateCommand` idempotent and cheap: they run on
  every recreation. Put heavy setup in the Dockerfile or features, which are
  cached in the image.
- Pin the base image (by tag or digest). DevPod reuses the built image until
  `devcontainer.json`, the Dockerfile or the features change, not when the base
  image changes upstream.
- Avoid `UV_LINK_MODE=symlink`: cleaning or pruning the cache then breaks
  existing virtual environments.
- Add a `.devpodignore` to the workspace root (`.dockerignore` syntax). It keeps
  files out of the upload of a local folder and out of the volume. Exclude
  anything the container rebuilds or that only fits your local OS:
  dependencies, virtual environments, caches and build output.

  ```gitignore
  **/node_modules
  **/.venv
  **/__pycache__
  **/.pytest_cache
  **/.mypy_cache
  **/.ruff_cache
  .uv-cache
  dist
  build
  ```

  Patterns are relative to the workspace root: `node_modules` only matches the
  top-level folder, `**/node_modules` matches it at any depth.

## Development

### Required environment variables

> These are pre-configured in Dev Containers

| Variable | Description | Example |
| --- | --- | --- |
| `DISK_IMAGE` | Hetzner image tag | `docker-ce` |
| `DISK_SIZE` | Disk size in GB | `30` |
| `GIT_REPO` | Git repo to download | `github.com/FKint/devpod-provider-hetzner` |
| `HCLOUD_TOKEN` | [Hetzner API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/) with `read & write` access | - |
| `MACHINE_FOLDER` | Local home folder | `~/.ssh` |
| `MACHINE_ID` | Unique identifier for the machine | `some-machine-id` |
| `MACHINE_TYPE` | Hetzner machine size | `cx32` |
| `REGION` | Hetzner region ID | `nbg1` |
| `TOKEN` | **Deprecated**. Replaced by `HCLOUD_TOKEN` | - |

### Testing independently of DevPod

To test the provider workflow, you can run the CLI commands directly.

| Command | Description | Example |
| --- | --- | --- |
| `command` | Run a command on the instance | `COMMAND="ls -la" go run . command` |
| `create` | Create an instance | `go run . create` |
| `delete` | Delete an instance and volume | `go run . delete` |
| `init` | Initialise an instance | `go run . init` |
| `start` | Start an instance | `go run . start` |
| `status` | Retrieve the status of an instance | `go run . status` |
| `stop` | Stop an instance | `go run . stop` |

### Testing in the DevPod ecosystem

> This assumes a Linux AMD64 workspace - if you're developing on any other machine
> please update the instructions for that machine (PRs welcome).
>
> These paths may differ on your machine.

To test the provider within the DevPod ecosystem:

1. Install the latest version of the [Hetzner provider](#usage)
1. Backup the original binary:

   ```shell
   mv ~/.devpod/contexts/default/providers/hetzner/binaries/hetzner_provider/devpod-provider-hetzner-linux-amd64 ~/.devpod/contexts/default/providers/hetzner/binaries/hetzner_provider/devpod-provider-hetzner-linux-amd64-orig
   ```

1. Build the binary:

   ```shell
   go build .
   ```

1. Move the new binary to the DevPod base:

   ```shell
   mv ./devpod-provider-hetzner ~/.devpod/contexts/default/providers/hetzner/binaries/hetzner_provider/devpod-provider-hetzner-linux-amd64
   ```

### Releasing

Releases are cut by pushing a `v*.*.*` tag. The [Build workflow](.github/workflows/build.yml)
then runs `commitlint`, `test`, `build` and `e2e` (real Hetzner servers), and
only when all pass does `publish` create the GitHub release with the binaries,
`provider.yaml` and a generated changelog.

One-time repository setup (Settings → Environments):

- **Hetzner E2E**: secrets `HCLOUD_TOKEN` (read & write) and `SSH_KEY`
- **Release**: secret `HCLOUD_TOKEN` with a **read-only** token, used only to
  generate `provider.yaml`

To cut a release:

1. Merge everything for the release into `main` and wait for its Build run to
   be green. All commits must pass commitlint (see [Commit style](#commit-style)).
1. Optionally dry-run the whole pipeline except `publish` (a non-tag run skips
   it): `gh workflow run build.yml --ref main`.
1. Optionally validate `publish` with a throwaway pre-release tag, then delete
   the release and tag afterwards:

   ```shell
   git tag v0.0.1-rc.1 && git push origin v0.0.1-rc.1
   gh run watch
   gh release delete v0.0.1-rc.1 --cleanup-tag --yes
   ```

1. Tag the release and push the tag:

   ```shell
   git checkout main && git pull
   git tag -a v1.2.3 -m "v1.2.3"
   git push origin v1.2.3
   ```

1. Watch the run (`gh run watch`) and approve the environment gates if prompted.
1. Check the release page lists all six binaries
   (`linux`/`windows`/`darwin` × `amd64`/`arm64`) and `provider.yaml`, then
   confirm DevPod can install it:

   ```shell
   devpod provider add https://github.com/FKint/devpod-provider-hetzner/releases/download/v1.2.3/provider.yaml
   ```

If a tag run fails, fix the cause, delete the tag
(`git push --delete origin v1.2.3 && git tag -d v1.2.3`) and push it again, or
use a new version. If e2e fails because Hetzner retired the `cpx22` machine
type, update `MACHINE_TYPE` in the workflow. Check Hetzner for leftover
`test-*` servers, volumes and SSH keys after any failed e2e run.

## Contributing

- Get a [Hetzner](https://hetzner.cloud/?ref=UWVUhEZNkm6p) account
- Get an [API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/)
  with `read & write` access
- Save this as `HCLOUD_TOKEN` in your `.envrc` file

### Open in a container

- [Open in a container](https://code.visualstudio.com/docs/devcontainers/containers)

### Commit style

All commits must be done in the [Conventional Commit](https://www.conventionalcommits.org)
format.

```git
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```
