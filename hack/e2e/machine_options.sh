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

# The machine options passed to `provider create` reach the server. The e2e job
# sets non-default values for them (SWAP_SIZE=2, CONTAINERS_SWAP_MAX=1G).

set -euo pipefail
. "$(dirname "$0")/lib.sh"

# SWAP_SIZE=2 (GB)
swap="$(remote "swapon --noheadings --bytes --show=SIZE" | tr -d '[:space:]')"
[ "${swap}" = "2147483648" ] || fail "swap is ${swap} bytes, expected 2147483648"

# CONTAINERS_SWAP_MAX=1G
swap_max="$(remote "systemctl show devpod.slice -p MemorySwapMax --value" | tr -d '[:space:]')"
[ "${swap_max}" = "1073741824" ] || fail "devpod.slice MemorySwapMax is ${swap_max}, expected 1073741824"
