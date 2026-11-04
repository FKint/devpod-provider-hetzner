#!/bin/bash
# Copyright 2026 github.com/FKint/devpod-provider-hetzner Authors
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

# Docker and containerd state lives on the Cloud Volume and survives a stop/start.
#
# Usage: persistence.sh before   (machine running, before the stop)
#        persistence.sh after    (machine recreated by the start)

set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

# A mirror of the official Docker Hub image, to stay clear of Docker Hub pull limits
IMAGE="public.ecr.aws/docker/library/alpine:3.20"
VOLUME="e2e-volume"
HOME_DIR="/home/devpod"
DATA_DIR="${HOME_DIR}/.devpod"
MARKER="${DATA_DIR}/e2e-marker"

assert_docker_root() {
  local root
  root="$(remote "docker info -f '{{.DockerRootDir}}'" | tr -d '[:space:]')"
  [ "${root}" = "${DATA_DIR}/docker" ] || fail "Docker root is ${root}, expected ${DATA_DIR}/docker"
}

case "${1:?usage: persistence.sh before|after}" in
before)
  # Where the state lives
  assert_docker_root

  remote "docker info -f '{{.DriverStatus}}'" | grep -F -- "io.containerd.snapshotter.v1" ||
    fail "Docker is not using the containerd image store"

  remote "systemctl cat containerd" | grep -F -- "--root ${DATA_DIR}/containerd" ||
    fail "containerd does not use the root on the volume"

  # Mounted from the volume, with the intended options
  remote "findmnt -no SOURCE,OPTIONS ${HOME_DIR}" | grep -F -- "noatime" ||
    fail "${HOME_DIR} is not mounted with noatime"

  # Create state to find again after the stop/start
  remote "docker pull -q ${IMAGE}"
  remote "docker volume create ${VOLUME} >/dev/null &&
    docker run --rm -v ${VOLUME}:/data ${IMAGE} sh -c 'echo persisted > /data/marker'"
  # sudo: ${DATA_DIR} is created by dockerd/containerd, owned by root
  remote "echo persisted | sudo tee ${MARKER} >/dev/null"
  ;;
after)
  [ "$(remote "sudo cat ${MARKER}" | tr -d '[:space:]')" = "persisted" ] ||
    fail "marker file on the volume did not survive"

  remote "docker image inspect ${IMAGE} >/dev/null" ||
    fail "image ${IMAGE} did not survive"

  # The image must not be pulled again; --pull=never makes a missing image fail
  [ "$(remote "docker run --rm --pull=never -v ${VOLUME}:/data ${IMAGE} cat /data/marker" | tr -d '[:space:]')" = "persisted" ] ||
    fail "named volume ${VOLUME} did not survive"

  assert_docker_root
  ;;
*)
  fail "usage: persistence.sh before|after"
  ;;
esac
