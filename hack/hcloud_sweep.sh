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

# Deletes e2e resources (label machineId=test-*) older than MAX_AGE_MINUTES (default 60).
# These are runs whose own cleanup never ran, e.g. a cancelled workflow. Only
# resources labelled machineId=test-... are touched, plus orphaned primary IPs:
# the ones Hetzner auto-creates for a server are named primary_ip-<id>, carry no
# labels and have auto_delete set, and can outlive an interrupted run.

set -uo pipefail

MAX_AGE_MINUTES="${MAX_AGE_MINUTES:-60}"
cutoff=$(( $(date +%s) - MAX_AGE_MINUTES * 60 ))
status=0

# Prints the ids of resources of the given kind that are stale test resources.
stale() {
  hcloud "$1" list -o json | jq -r --argjson cutoff "${cutoff}" '
    .[]
    | select((.labels.machineId // "") | startswith("test-"))
    | select((.created | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdateiso8601) < $cutoff)
    | .id'
}

for id in $(stale server); do
  echo "Deleting stale server ${id}"
  hcloud server delete "${id}" || status=1
done

for id in $(stale volume); do
  echo "Deleting stale volume ${id}"
  hcloud volume detach "${id}" || true
  hcloud volume delete "${id}" || status=1
done

# Primary IPs outlive a server whose create was interrupted, either unassigned
# or still pointing at a server that no longer exists (a dangling assignment,
# which Hetzner neither releases nor auto-deletes). IPs assigned to a live
# server are never touched. Matched by label machineId=test-*, or as
# auto-created (auto_delete, generic primary_ip-<id> name); IPs created by hand
# have auto_delete off by default and are left alone.
stale_primary_ips() {
  local servers
  servers=$(hcloud server list -o json | jq -c '[.[].id]')
  hcloud primary-ip list -o json | jq -r --argjson cutoff "${cutoff}" --argjson servers "${servers}" '
    .[]
    | select(.assignee_id == null or (.assignee_id as $a | $servers | index($a) | not))
    | select(((.labels.machineId // "") | startswith("test-")) or (.auto_delete and (.name | test("^primary_ip-[0-9]+$"))))
    | select((.created | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdateiso8601) < $cutoff)
    | .id'
}

for id in $(stale_primary_ips); do
  echo "Deleting stale primary IP ${id}"
  # A dangling assignment must be cleared first; this fails harmlessly if unassigned.
  hcloud primary-ip unassign "${id}" 2>/dev/null || true
  hcloud primary-ip delete "${id}" || status=1
done

for id in $(stale ssh-key); do
  echo "Deleting stale SSH key ${id}"
  hcloud ssh-key delete "${id}" || status=1
done

exit "${status}"
