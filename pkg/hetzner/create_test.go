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
	"strconv"
	"testing"
	"time"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	reqCreateVolume = "POST /volumes"
	reqCreateServer = "POST /servers"
	reqDeleteServer = "DELETE /servers/*"
	reqDeleteVolume = "DELETE /volumes/*"
	reqDeleteKey    = "DELETE /ssh_keys/*"
	reqPoweron      = "POST /servers/*/actions/poweron"
	reqResize       = "POST /volumes/*/actions/resize"
)

func testRequest() *hcloud.ServerCreateOpts {
	return &hcloud.ServerCreateOpts{
		Name:       machineName,
		Location:   &hcloud.Location{ID: 1, Name: fakeRegion},
		ServerType: &hcloud.ServerType{ID: 2, Name: fakeMachineType, Disk: fakeServerDisk},
		Image:      &hcloud.Image{ID: 3, Name: fakeImage},
		Labels:     machineLabels(machineName),
	}
}

func testMachineOptions(t *testing.T) *options.Machine {
	t.Helper()

	return defaultMachine(t)
}

// createMachine runs Create against the fakes with a 10GB disk.
func createMachine(t *testing.T, cloud *fakeCloud, key []byte, diskSize int) error {
	t.Helper()

	return cloud.hetzner(t).Create(boundedCtx(t), testRequest(), diskSize, testMachineOptions(t), "ssh-ed25519 AAAA test", key)
}

func assertNothingLeft(t *testing.T, cloud *fakeCloud) {
	t.Helper()
	servers, volumes, keys := cloud.left()
	assert.Empty(t, servers, "servers left behind")
	assert.Empty(t, volumes, "volumes left behind")
	assert.Empty(t, keys, "SSH keys left behind")
}

func TestCreateNewMachine(t *testing.T) {
	cloud := newFakeCloud()
	_, key := newFakeSSHD(t, statusRunning, "done")

	require.NoError(t, createMachine(t, cloud, key, 10))

	servers, volumes, _ := cloud.left()
	assert.Equal(t, []string{machineName}, servers)
	assert.Equal(t, []string{machineName}, volumes)

	var vol *cloudVolume
	for _, v := range cloud.volumes {
		vol = v
	}
	assert.Equal(t, 10, vol.Size)
	assert.NotZero(t, vol.Server, "the volume is attached to the new server")
	userData, _ := cloud.lastServerBody["user_data"].(string)
	assert.Contains(t, userData, "scsi-0HC_Volume_"+strconv.FormatInt(vol.ID, 10), "the server mounts the volume it was created with")
	assert.Zero(t, cloud.count(reqDeleteServer)+cloud.count(reqDeleteVolume), "a successful create deletes nothing")
}

func TestCreateReusesAndGrowsAnExistingVolume(t *testing.T) {
	cloud := newFakeCloud()
	vol := cloud.seedVolume(machineName, 10)
	_, key := newFakeSSHD(t, "done")

	require.NoError(t, createMachine(t, cloud, key, 20))

	assert.Zero(t, cloud.count(reqCreateVolume), "the data volume is reused")
	assert.Equal(t, 1, cloud.count(reqResize))
	assert.Equal(t, 20, cloud.volumes[vol.ID].Size)
	assert.Equal(t, 1, cloud.count(reqCreateServer))
}

func TestCreateNeverShrinksAnExistingVolumeAndIgnoresTheMinimum(t *testing.T) {
	cloud := newFakeCloud()
	vol := cloud.seedVolume(machineName, 40)
	_, key := newFakeSSHD(t, "done")

	// 5GB is below the minimum for a new volume, but this one exists
	require.NoError(t, createMachine(t, cloud, key, 5))

	assert.Zero(t, cloud.count(reqResize))
	assert.Equal(t, 40, cloud.volumes[vol.ID].Size)
}

func TestCreateRefusesTooSmallNewVolume(t *testing.T) {
	cloud := newFakeCloud()
	_, key := newFakeSSHD(t)

	err := createMachine(t, cloud, key, options.MinDiskSizeGB-1)
	assert.ErrorIs(t, err, ErrDiskTooSmall)
	assert.Zero(t, cloud.count(reqCreateVolume))
	assertNothingLeft(t, cloud)
}

func TestCreateRefusesSwapThatDoesNotFitBeforeTouchingAnything(t *testing.T) {
	cloud := newFakeCloud()
	_, key := newFakeSSHD(t)
	machine := testMachineOptions(t)
	machine.SwapSizeBytes = int64(fakeServerDisk) << 30

	err := cloud.hetzner(t).Create(context.Background(), testRequest(), 10, machine, "ssh-ed25519 AAAA test", key)
	assert.ErrorIs(t, err, ErrSwapTooLarge)
	assert.Zero(t, cloud.count("GET /volumes"), "refused before waiting on anything")
	assert.Zero(t, cloud.count(reqCreateVolume)+cloud.count(reqCreateServer))
}

func TestCreateRollsBackEverythingItCreated(t *testing.T) {
	tests := []struct {
		name     string
		fail     []string
		statuses []string
	}{
		{name: "volume creation fails", fail: []string{reqCreateVolume}},
		{name: "server creation fails", fail: []string{reqCreateServer}},
		{name: "cloud-init fails", statuses: []string{"error"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloud := newFakeCloud()
			cloud.seedKey(machineName)
			cloud.seedKey(otherName)
			cloud.fail(tt.fail...)
			_, key := newFakeSSHD(t, tt.statuses...)

			err := createMachine(t, cloud, key, 10)
			require.Error(t, err)

			servers, volumes, keys := cloud.left()
			assert.Empty(t, servers)
			assert.Empty(t, volumes, "the new volume holds no data yet, so it is removed")
			assert.Equal(t, []string{"other-key"}, keys, "only this machine's SSH keys are removed")
		})
	}
}

func TestCreateReportsTheOriginalErrorAfterRollback(t *testing.T) {
	cloud := newFakeCloud()
	_, key := newFakeSSHD(t, "error")
	assert.ErrorIs(t, createMachine(t, cloud, key, 10), ErrProvisioningFailed)
}

func TestCreateRollbackKeepsAnExistingVolume(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedVolume(machineName, 10)
	cloud.seedKey(machineName)
	_, key := newFakeSSHD(t, "error")

	require.Error(t, createMachine(t, cloud, key, 10))

	servers, volumes, keys := cloud.left()
	assert.Empty(t, servers, "the server that failed to provision is removed")
	assert.Equal(t, []string{machineName}, volumes, "a volume that already existed holds the user's data")
	assert.Empty(t, keys)
	assert.Zero(t, cloud.count(reqDeleteVolume))
}

func TestCreateRollbackStillRunsWhenTheContextIsGone(t *testing.T) {
	cloud := newFakeCloud()
	_, key := newFakeSSHD(t, statusRunning)

	// Ctrl-C or a deadline while waiting for provisioning must not leave billable resources
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := cloud.hetzner(t).Create(ctx, testRequest(), 10, testMachineOptions(t), "ssh-ed25519 AAAA test", key)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assertNothingLeft(t, cloud)
}

func TestCreateRollbackKeepsTheVolumeWhenTheServerCannotBeDeleted(t *testing.T) {
	cloud := newFakeCloud()
	cloud.fail(reqDeleteServer)
	_, key := newFakeSSHD(t, "error")

	err := createMachine(t, cloud, key, 10)
	assert.ErrorIs(t, err, ErrProvisioningFailed, "the caller sees the original error, not the cleanup's")

	servers, volumes, _ := cloud.left()
	assert.Equal(t, []string{machineName}, servers, "left for the user to remove, as the log says")
	assert.Equal(t, []string{machineName}, volumes, "a volume can't be removed while attached")
	assert.Zero(t, cloud.count(reqDeleteVolume))
}

func TestCreateRollbackStillRemovesKeysWhenTheVolumeCannotBeDeleted(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedKey(machineName)
	cloud.fail(reqDeleteVolume)
	_, key := newFakeSSHD(t, "error")

	assert.ErrorIs(t, createMachine(t, cloud, key, 10), ErrProvisioningFailed)

	servers, volumes, keys := cloud.left()
	assert.Empty(t, servers)
	assert.Equal(t, []string{machineName}, volumes)
	assert.Empty(t, keys)
}

func TestCreateOnRunningServerOnlyWaitsForProvisioning(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	d, key := newFakeSSHD(t, "done")

	require.NoError(t, createMachine(t, cloud, key, 10))

	assert.Zero(t, cloud.count(reqCreateServer)+cloud.count(reqCreateVolume)+cloud.count(reqPoweron))
	assert.Equal(t, 1, cloudInitChecks(d))
}

func TestCreateOnPoweredOffServerPowersItOn(t *testing.T) {
	cloud := newFakeCloud()
	srv := cloud.seedServer(machineName, "off")
	_, key := newFakeSSHD(t, "done")

	require.NoError(t, createMachine(t, cloud, key, 10))

	assert.Equal(t, 1, cloud.count(reqPoweron))
	assert.Equal(t, statusRunning, cloud.servers[srv.ID].Status)
}

func TestCreateOnPoweredOffServerReportsAFailedPowerOn(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, "off")
	cloud.fail(reqPoweron)
	_, key := newFakeSSHD(t)

	err := createMachine(t, cloud, key, 10)
	assert.ErrorContains(t, err, "power on server")
	servers, _, _ := cloud.left()
	assert.Equal(t, []string{machineName}, servers)
}

func TestCreateOnBusyServerAsksToRetry(t *testing.T) {
	for _, status := range []string{"stopping", "deleting", "migrating"} {
		t.Run(status, func(t *testing.T) {
			cloud := newFakeCloud()
			cloud.seedServer(machineName, status)
			_, key := newFakeSSHD(t)

			assert.ErrorIs(t, createMachine(t, cloud, key, 10), ErrServerBusy)
			assert.Zero(t, cloud.count(reqPoweron)+cloud.count(reqDeleteServer))
		})
	}
}

// Never delete an existing server on a failed start: the user may still be using it
func TestCreateOnExistingServerNeverDeletesIt(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.seedVolume(machineName, 10)
	cloud.seedKey(machineName)
	d, key := newFakeSSHD(t)
	d.authorized = nil // e.g. the machine's key pair was regenerated

	err := createMachine(t, cloud, key, 10)

	assert.ErrorIs(t, err, ErrProvisioningFailed)
	assert.ErrorContains(t, err, "left in place")
	servers, volumes, keys := cloud.left()
	assert.Equal(t, []string{machineName}, servers)
	assert.Equal(t, []string{machineName}, volumes)
	assert.Equal(t, []string{"machine-key"}, keys)
	assert.Zero(t, cloud.count(reqDeleteServer)+cloud.count(reqDeleteVolume)+cloud.count(reqDeleteKey))
}

func TestCreateRefusesAmbiguousMachines(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.seedServer(machineName, statusRunning)
	_, key := newFakeSSHD(t)

	assert.ErrorContains(t, createMachine(t, cloud, key, 10), ErrMultipleServersFound(machineName).Error())
}

func TestCreateReportsAnAPIFailureWithoutCreatingAnything(t *testing.T) {
	cloud := newFakeCloud()
	cloud.fail("GET /servers")
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	assert.Zero(t, cloud.count(reqCreateVolume)+cloud.count(reqCreateServer))
}
