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

# A swap file that cannot fit on the server type's local disk is refused before
# any server or volume is created. Run before the first `provider create`.

set -euo pipefail
. "$(dirname "$0")/lib.sh"

# cpx22 has an 80 GB disk; 100 GB of swap can never fit
out="$(SWAP_SIZE=100 "${PROVIDER}" create 2>&1)" && fail "create succeeded with SWAP_SIZE=100"
echo "${out}"
echo "${out}" | grep -i -- "swap" || fail "the error does not mention the swap size"

for kind in server volume; do
  left="$(hcloud "${kind}" list -l "machineId=${MACHINE_ID}" -o noheader -o columns=id)"
  [ -z "${left}" ] || fail "a ${kind} was created despite the rejected swap size: ${left}"
done
