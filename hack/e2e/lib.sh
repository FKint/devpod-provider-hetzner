#!/bin/bash
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

# Shared helpers for the e2e scripts. Source this file; do not execute it.
# Expects the provider binary at ./provider and the provider env vars
# (HCLOUD_TOKEN, MACHINE_ID, ...) to be set, as in the e2e job.

set -euo pipefail

PROVIDER="${PROVIDER:-./provider}"

fail() {
  echo "::error::$*" >&2
  exit 1
}

# Runs a shell command on the machine through the provider.
remote() {
  COMMAND="$1" "${PROVIDER}" command
}

# Fails unless the provider reports the expected machine status.
assert_status() {
  local expected="$1" actual
  actual="$("${PROVIDER}" status)"
  echo "status: ${actual} (expected ${expected})"
  [ "${actual}" = "${expected}" ] || fail "expected status ${expected}, got ${actual}"
}
