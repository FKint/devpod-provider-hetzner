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
	"time"

	"github.com/loft-sh/devpod/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteRemovesTheMachineAndOnlyTheMachine(t *testing.T) {
	cloud := newFakeCloud()
	srv := cloud.seedServer(machineName, statusRunning)
	vol := cloud.seedVolume(machineName, 10)
	vol.Server = srv.ID
	cloud.seedKey(machineName)
	cloud.seedKey(machineName)
	cloud.seedKey(otherName)
	other := cloud.seedServer(otherName, statusRunning)
	cloud.seedVolume(otherName, 10)

	require.NoError(t, cloud.hetzner(t).Delete(context.Background(), machineName))

	servers, volumes, keys := cloud.left()
	assert.Equal(t, []string{otherName}, servers)
	assert.Equal(t, []string{otherName}, volumes)
	assert.Equal(t, []string{"other-key"}, keys)
	assert.Contains(t, cloud.servers, other.ID)
}

// The server must go before the volume (it can't be deleted while attached), and the
// keys last, so a failed delete leaves a machine that can still be reached.
func TestDeleteOrder(t *testing.T) {
	cloud := newFakeCloud()
	srv := cloud.seedServer(machineName, statusRunning)
	cloud.seedVolume(machineName, 10).Server = srv.ID
	cloud.seedKey(machineName)

	require.NoError(t, cloud.hetzner(t).Delete(context.Background(), machineName))

	server, volume, key := cloud.order(reqDeleteServer), cloud.order(reqDeleteVolume), cloud.order(reqDeleteKey)
	assert.True(t, server >= 0 && server < volume && volume < key, "server %d, volume %d, key %d", server, volume, key)
}

func TestDeleteWhenOnlyTheVolumeIsLeft(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedVolume(machineName, 10)
	cloud.seedKey(machineName)

	require.NoError(t, cloud.hetzner(t).Delete(context.Background(), machineName))
	assertNothingLeft(t, cloud)
}

func TestDeleteIsIdempotent(t *testing.T) {
	cloud := newFakeCloud()
	h := cloud.hetzner(t)
	assert.NoError(t, h.Delete(context.Background(), machineName), "nothing to delete")

	cloud.seedServer(machineName, statusRunning)
	cloud.seedVolume(machineName, 10)
	require.NoError(t, h.Delete(context.Background(), machineName))
	assert.NoError(t, h.Delete(context.Background(), machineName), "a second delete is a no-op")
}

func TestDeleteCanBeRetriedAfterAFailure(t *testing.T) {
	for _, failing := range []string{reqDeleteServer, reqDeleteVolume, reqDeleteKey} {
		t.Run(failing, func(t *testing.T) {
			cloud := newFakeCloud()
			srv := cloud.seedServer(machineName, statusRunning)
			cloud.seedVolume(machineName, 10).Server = srv.ID
			cloud.seedKey(machineName)
			h := cloud.hetzner(t)

			cloud.fail(failing)
			assert.Error(t, h.Delete(context.Background(), machineName), "the caller must learn that the delete failed")
			assert.Equal(t, 1, cloud.count(failing))

			cloud.heal()
			require.NoError(t, h.Delete(context.Background(), machineName))
			assertNothingLeft(t, cloud)
		})
	}
}

func TestDeleteKeepsTheKeysWhileTheVolumeRemains(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedVolume(machineName, 10)
	cloud.seedKey(machineName)
	cloud.fail(reqDeleteVolume)

	assert.Error(t, cloud.hetzner(t).Delete(context.Background(), machineName))
	_, _, keys := cloud.left()
	assert.Equal(t, []string{"machine-key"}, keys)
}

func TestDeleteRefusesAmbiguousMachines(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.seedServer(machineName, statusRunning)

	assert.ErrorContains(t, cloud.hetzner(t).Delete(context.Background(), machineName), "multiple server")
	assert.Zero(t, cloud.count(reqDeleteServer))
}

func TestDeleteSSHKeysOnlyRemovesThoseOfTheMachine(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedKey(machineName)
	cloud.seedKey("machine-2")
	require.NoError(t, cloud.hetzner(t).deleteSSHKeys(context.Background(), machineName))
	_, _, keys := cloud.left()
	assert.Equal(t, []string{"machine-2-key"}, keys)
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeCloud)
		want  client.Status
	}{
		{"nothing", func(*fakeCloud) {}, client.StatusNotFound},
		{"running server", func(c *fakeCloud) { c.seedServer(machineName, statusRunning) }, client.StatusRunning},
		{"powered off server", func(c *fakeCloud) { c.seedServer(machineName, "off") }, client.StatusStopped},
		{"starting server", func(c *fakeCloud) { c.seedServer(machineName, "starting") }, client.StatusBusy},
		{"stopping server", func(c *fakeCloud) { c.seedServer(machineName, "stopping") }, client.StatusBusy},
		{"only the volume", func(c *fakeCloud) { c.seedVolume(machineName, 10) }, client.StatusStopped},
		{"only another machine", func(c *fakeCloud) { c.seedServer(otherName, statusRunning) }, client.StatusNotFound},
		{
			"volume still released by a deleted server",
			func(c *fakeCloud) { c.seedVolume(machineName, 10).Server = 999 },
			client.StatusBusy,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloud := newFakeCloud()
			tt.setup(cloud)
			got, err := cloud.hetzner(t).Status(context.Background(), machineName)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatusReportsAPIErrors(t *testing.T) {
	for _, failing := range []string{"GET /servers", "GET /volumes"} {
		cloud := newFakeCloud()
		cloud.fail(failing)
		_, err := cloud.hetzner(t).Status(context.Background(), machineName)
		assert.Error(t, err, failing)
	}
}

func TestWaitForStatusReturnsOnceReached(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	assert.NoError(t, cloud.hetzner(t).WaitForStatus(context.Background(), machineName, client.StatusRunning))
}

func TestWaitForStatusFollowsTheServer(t *testing.T) {
	cloud := newFakeCloud()
	srv := cloud.seedServer(machineName, "stopping")
	go func() {
		time.Sleep(20 * time.Millisecond)
		cloud.mu.Lock()
		defer cloud.mu.Unlock()
		delete(cloud.servers, srv.ID)
	}()

	assert.NoError(t, cloud.hetzner(t).WaitForStatus(context.Background(), machineName, client.StatusNotFound))
}

func TestWaitForStatusRetriesAPIErrors(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.fail("GET /servers")
	go func() {
		time.Sleep(20 * time.Millisecond)
		cloud.heal()
	}()

	assert.NoError(t, cloud.hetzner(t).WaitForStatus(context.Background(), machineName, client.StatusRunning))
	assert.Greater(t, cloud.count("GET /servers"), 1)
}

func TestWaitForStatusTimeoutSaysWhereItGotStuck(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, "starting")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := cloud.hetzner(t).WaitForStatus(ctx, machineName, client.StatusRunning)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, `waiting for status "Running" (currently "Busy")`)
}

func TestWaitForStatusTimeoutReportsTheLastAPIError(t *testing.T) {
	cloud := newFakeCloud()
	cloud.fail("GET /servers")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := cloud.hetzner(t).WaitForStatus(ctx, machineName, client.StatusRunning)
	assert.ErrorContains(t, err, "injected failure")
	assert.ErrorContains(t, err, `waiting for status "Running"`)
}

func TestInit(t *testing.T) {
	cloud := newFakeCloud()
	assert.NoError(t, cloud.hetzner(t).Init(context.Background()))

	cloud.fail("GET /servers")
	assert.Error(t, cloud.hetzner(t).Init(context.Background()), "a bad token or unreachable API fails init")
}

func TestNewHetzner(t *testing.T) {
	h := NewHetzner("token")
	assert.NotNil(t, h.client)
	assert.Equal(t, statusPoll, h.pollInterval(), "defaults apply unless a test shortens them")
}
