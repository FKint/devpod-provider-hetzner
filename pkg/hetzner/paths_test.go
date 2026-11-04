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

func TestCreateRollsBackWhenTheVolumeActionFails(t *testing.T) {
	cloud := newFakeCloud()
	cloud.failAction(reqCreateVolume)
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	assertNothingLeft(t, cloud)
}

// An existing volume that can't be grown is an error, and is kept
func TestCreateStopsWhenTheVolumeResizeIsUnconfirmed(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedVolume(machineName, 10)
	cloud.failAction(reqResize)
	_, key := newFakeSSHD(t)

	assert.ErrorContains(t, createMachine(t, cloud, key, 20), "growing volume")
	servers, volumes, _ := cloud.left()
	assert.Empty(t, servers)
	assert.Equal(t, []string{machineName}, volumes)
}

func TestCreateRefusesAVolumeThatBelongsToAnotherServer(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedVolume(machineName, 10).Server = cloud.seedServer(otherName, statusRunning).ID
	_, key := newFakeSSHD(t)

	assert.ErrorIs(t, createMachine(t, cloud, key, 10), ErrVolumeAttached)
	assert.Zero(t, cloud.count(reqCreateServer))
}

// When the retry after a failed attach can't check the volume, the error is reported
func TestCreateReportsAVolumeCheckFailingDuringTheAttachRetry(t *testing.T) {
	cloud := newFakeCloud()
	cloud.attachFailures = 1
	cloud.failAfter(reqListVolumes, 1)
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	assert.Equal(t, 1, cloud.count(reqCreateServer), "no second server without a settled volume")
}

func TestCreateOnPoweredOffServerReportsAFailedPowerOnAction(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, "off")
	cloud.failAction(reqPoweron)
	_, key := newFakeSSHD(t)

	err := createMachine(t, cloud, key, 10)
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "power on server", "the request worked, its action failed")
}

// The error of the create is what the caller sees, even if the cleanup fails as well
func TestCreateRollbackReportsKeysThatCannotBeDeleted(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedKey(machineName)
	cloud.fail(reqCreateServer, reqDeleteKey)
	_, key := newFakeSSHD(t)

	err := createMachine(t, cloud, key, 10)
	assert.ErrorContains(t, err, reqCreateServer)
	_, _, keys := cloud.left()
	assert.Len(t, keys, 1)
}

// A create whose answer got lost may still have made the server: it must be cleaned up
func TestCreateRemovesAServerWhoseResponseWasLost(t *testing.T) {
	cloud := newFakeCloud()
	cloud.loseResponse(reqCreateServer)
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	assert.Equal(t, 1, cloud.count(reqCreateServer))
	assertNothingLeft(t, cloud)
}

func TestDeleteVolumeReportsAFailedDetachAction(t *testing.T) {
	cloud := newFakeCloud()
	seedAttachedMachine(cloud)
	cloud.failAction(reqDetachVolume)

	assert.Error(t, cloud.hetzner(t).deleteVolume(context.Background(), machineName))
	assert.Zero(t, cloud.count(reqDeleteVolume))
}

func TestAttemptConnectionIgnoresAFailingDiskCheck(t *testing.T) {
	d, key := newFakeSSHD(t, "done")
	d.dfFails = true

	status, err := attemptConnection(context.Background(), fakeServerIP, key)
	require.NoError(t, err)
	assert.Equal(t, "done", status.Status)
	assert.Len(t, d.ran(), 2, "the disk check ran and failed")
}
