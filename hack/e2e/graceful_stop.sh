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

# A stop shuts the server down cleanly before deleting it, so data that was
# written but not yet flushed reaches the volume.
#
# Usage: graceful_stop.sh before   (machine running; run right before the stop)
#        graceful_stop.sh after    (machine recreated by the start)
#
# The file is written WITHOUT sync. Linux writes dirty pages back after ~30 s,
# so deleting the server within seconds of the write loses it unless the
# shutdown flushed the filesystem first.

set -euo pipefail
. "$(dirname "$0")/lib.sh"

FILE="/home/devpod/e2e-unsynced"

case "${1:?usage: graceful_stop.sh before|after}" in
before)
  remote "head -c 1048576 /dev/zero | tr '\\0' 'x' > ${FILE} && echo written"
  ;;
after)
  size="$(remote "stat -c %s ${FILE}" | tr -d '[:space:]')"
  [ "${size}" = "1048576" ] || fail "${FILE} is ${size} bytes after the stop, expected 1048576 (unflushed data was lost)"
  ;;
*)
  fail "usage: graceful_stop.sh before|after"
  ;;
esac
