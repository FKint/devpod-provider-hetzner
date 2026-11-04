#!/usr/bin/env bash
# Copyright 2023 Simon Emms <simon@simonemms.com>
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -euo pipefail

# The platforms listed in hack/provider/main.go; keep the two in sync
PLATFORMS="${PROVIDER_BUILD_PLATFORMS:-linux windows darwin}"
ARCHS="${PROVIDER_BUILD_ARCHS:-amd64 arm64}"

cd "$(git rev-parse --show-toplevel)"

# Set by CI; a local build has no version to stamp
GIT_REPO="${GIT_REPO:-github.com/FKint/devpod-provider-hetzner}"
RELEASE_VERSION="${RELEASE_VERSION:-development}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD)}"
LDFLAGS="-w -s -X ${GIT_REPO}/cmd.Version=${RELEASE_VERSION} -X ${GIT_REPO}/cmd.GitCommit=${GIT_COMMIT}"

mkdir -p dist

for OS in ${PLATFORMS}; do
  for ARCH in ${ARCHS}; do
    # No Windows on ARM binary is published
    if [[ "${OS}" == "windows" && "${ARCH}" == "arm64" ]]; then
      echo "Skipping ${OS}/${ARCH}"
      continue
    fi

    NAME="devpod-provider-hetzner-${OS}-${ARCH}"
    if [[ "${OS}" == "windows" ]]; then
      NAME="${NAME}.exe"
    fi

    echo "Building for ${OS}/${ARCH}"
    GOOS="${OS}" GOARCH="${ARCH}" CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "dist/${NAME}" .
    shasum -a 256 "dist/${NAME}" | cut -d ' ' -f 1 > "dist/${NAME}.sha256"
  done
done

# generate provider.yaml - this queries the Hetzner API so needs a token. CI sets
# SKIP_PROVIDER_YAML=true and generates it in the publish job instead, so the
# token is only exposed to the release environment. Otherwise only skip for
# development builds (eg, local builds); releases must fail loudly
if [[ "${SKIP_PROVIDER_YAML:-}" == "true" ]]; then
  echo "SKIP_PROVIDER_YAML set - skipping provider.yaml generation"
elif [[ -z "${HCLOUD_TOKEN:-}" && "${RELEASE_VERSION}" == "development" ]]; then
  echo "HCLOUD_TOKEN not set - skipping provider.yaml generation for development build"
else
  go run ./hack/provider "${RELEASE_VERSION}" > dist/provider.yaml
fi
