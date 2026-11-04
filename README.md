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
| `MACHINE_TYPE` | Hetzner machine size | `cx33` |
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
| `stop` | Stop an instance. Shuts the server down gracefully first, then deletes it (the volume is kept) | `go run . stop` |

`stop --from-machine` is what the inactivity shutdown runs on the machine itself. A power-off there
would kill the command before it could delete the server, so it stops the containers and syncs the
disks instead.

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
