/*
 * Copyright 2026 github.com/FKint/devpod-provider-hetzner Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package hetzner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	reqListServers  = "GET /servers"
	reqGetServer    = "GET /servers/*"
	reqListVolumes  = "GET /volumes"
	reqListKeys     = "GET /ssh_keys"
	reqDetachVolume = "POST /volumes/*/actions/detach"
	reqShutdown     = "POST /servers/*/actions/shutdown"
)

// seedAttachedMachine seeds a running machine with a volume attached to it and an SSH key.
func seedAttachedMachine(cloud *fakeCloud) {
	srv := cloud.seedServer(machineName, statusRunning)
	cloud.seedVolume(machineName, 10).Server = srv.ID
	cloud.seedKey(machineName)
}

// A failing lookup must be reported, not mistaken for "nothing to delete", and must
// stop the delete before it removes anything that depends on the lookup.
func TestDeleteReportsFailingLookups(t *testing.T) {
	tests := []struct {
		failing string
		// leftAfter is what must still exist: the delete stops at the failing step
		servers, volumes, keys int
	}{
		{reqListServers, 1, 1, 1},
		{reqListVolumes, 0, 1, 1},
		{reqListKeys, 0, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.failing, func(t *testing.T) {
			cloud := newFakeCloud()
			seedAttachedMachine(cloud)
			cloud.fail(tt.failing)

			assert.Error(t, cloud.hetzner(t).Delete(context.Background(), machineName))

			servers, volumes, keys := cloud.left()
			assert.Len(t, servers, tt.servers)
			assert.Len(t, volumes, tt.volumes)
			assert.Len(t, keys, tt.keys)

			cloud.heal()
			require.NoError(t, cloud.hetzner(t).Delete(context.Background(), machineName))
			assertNothingLeft(t, cloud)
		})
	}
}

func TestDeleteVolumeReportsAFailingServerLookup(t *testing.T) {
	cloud := newFakeCloud()
	seedAttachedMachine(cloud)
	cloud.fail(reqGetServer)

	assert.Error(t, cloud.hetzner(t).deleteVolume(context.Background(), machineName))
	assert.Zero(t, cloud.count(reqDeleteVolume), "the volume must not be deleted while its holder is unknown")
}

func TestDeleteVolumeReportsAFailingDetach(t *testing.T) {
	cloud := newFakeCloud()
	seedAttachedMachine(cloud)
	cloud.fail(reqDetachVolume)

	assert.ErrorContains(t, cloud.hetzner(t).deleteVolume(context.Background(), machineName), "detach volume")
	assert.Zero(t, cloud.count(reqDeleteVolume))
}

func TestStopReportsAFailingServerLookup(t *testing.T) {
	cloud := newFakeCloud()
	seedAttachedMachine(cloud)
	cloud.fail(reqListServers)
	h := cloud.hetzner(t)
	h.runLocal = func(context.Context, string, ...string) error { return nil }

	assert.Error(t, h.StopFromClient(context.Background(), machineName))
	assert.Error(t, h.StopFromMachine(context.Background(), machineName))
	assert.Zero(t, cloud.count(reqDeleteServer))
}

func TestStopOfAnUnknownMachineIsANoOp(t *testing.T) {
	cloud := newFakeCloud()
	h := cloud.hetzner(t)
	h.runLocal = func(context.Context, string, ...string) error {
		t.Error("nothing to quiesce when there is no server")
		return nil
	}

	assert.NoError(t, h.StopFromClient(context.Background(), machineName))
	assert.NoError(t, h.StopFromMachine(context.Background(), machineName))
	assert.Zero(t, cloud.count(reqDeleteServer))
}

// A failing delete request (not a failed action) is not retried, and is reported
func TestStopReportsAFailingDelete(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.fail(reqDeleteServer)

	assert.Error(t, cloud.hetzner(t).StopFromClient(context.Background(), machineName))
	assert.Equal(t, 1, cloud.count(reqDeleteServer))
}

// If the server's status can't be read while waiting for the power-off, the stop
// still ends with the server deleted
func TestStopDeletesWhenTheStatusCannotBeRead(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.fail(reqGetServer)

	require.NoError(t, cloud.hetzner(t).StopFromClient(context.Background(), machineName))
	assert.Positive(t, cloud.count(reqGetServer), "the status was polled")
	servers, _, _ := cloud.left()
	assert.Empty(t, servers)
}

func TestStopDeletesWhenTheShutdownRequestFails(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.fail(reqShutdown)

	require.NoError(t, cloud.hetzner(t).StopFromClient(context.Background(), machineName))
	servers, _, _ := cloud.left()
	assert.Empty(t, servers)
}

func TestStopSkipsTheShutdownOfAServerThatIsNotRunning(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, "off")

	require.NoError(t, cloud.hetzner(t).StopFromClient(context.Background(), machineName))
	assert.Zero(t, cloud.count(reqShutdown))
	servers, _, _ := cloud.left()
	assert.Empty(t, servers)
}
