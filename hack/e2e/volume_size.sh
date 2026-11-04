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

# New volumes are XFS, and raising DISK_SIZE enlarges the volume and grows the
# filesystem when the machine is started again.
#
# Usage: volume_size.sh before <size-gb>   (machine running)
#        volume_size.sh after <size-gb>    (machine recreated with DISK_SIZE=<size-gb>)

set -euo pipefail
. "$(dirname "$0")/lib.sh"

MOUNT="/home/devpod"
STATE="${MACHINE_FOLDER:?}/fs_size_before"

fs_bytes() {
  remote "df -B1 --output=size ${MOUNT} | tail -n 1" | tr -d '[:space:]'
}

volume_gb() {
  hcloud volume list -l "machineId=${MACHINE_ID}" -o noheader -o columns=size | tr -d '[:space:]'
}

mode="${1:?usage: volume_size.sh before|after <size-gb>}"
want="${2:?usage: volume_size.sh before|after <size-gb>}"

[ "$(volume_gb)" = "${want}" ] ||
  fail "volume is $(volume_gb) GB, expected ${want} GB"

case "${mode}" in
before)
  fstype="$(remote "findmnt -n -o FSTYPE ${MOUNT}" | tr -d '[:space:]')"
  [ "${fstype}" = "xfs" ] || fail "${MOUNT} is ${fstype}, expected xfs for a new volume"
  fs_bytes > "${STATE}"
  echo "filesystem: $(cat "${STATE}") bytes on a ${want} GB volume"
  ;;
after)
  before="$(cat "${STATE}")"
  after="$(fs_bytes)"
  echo "filesystem: ${before} -> ${after} bytes on a ${want} GB volume"
  [ "${after}" -gt "${before}" ] || fail "filesystem did not grow (${before} -> ${after} bytes)"
  ;;
*)
  fail "usage: volume_size.sh before|after <size-gb>"
  ;;
esac
