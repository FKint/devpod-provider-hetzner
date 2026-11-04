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
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/devpod/pkg/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer() *hcloud.Server {
	return &hcloud.Server{
		Name:      machineName,
		PublicNet: hcloud.ServerPublicNet{IPv4: hcloud.ServerPublicNetIPv4{IP: net.ParseIP(fakeServerIP)}},
	}
}

// boundedCtx stops a regression that makes a poll endless from hanging the suite: the
// polls end at their own deadline (10 minutes in production), the test fails at this one.
func boundedCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	return ctx
}

func cloudInitChecks(d *fakeSSHD) int {
	n := 0
	for _, c := range d.ran() {
		if strings.HasPrefix(c, "cloud-init status") {
			n++
		}
	}

	return n
}

func TestDialSSHRunsCommands(t *testing.T) {
	d, key := newFakeSSHD(t)
	conn, err := DialSSH(context.Background(), fakeServerIP, key)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	out := new(bytes.Buffer)
	require.NoError(t, ssh.Run(context.Background(), conn, "cloud-init status", &bytes.Buffer{}, out, &bytes.Buffer{}, nil))
	assert.Equal(t, "status: running\n", out.String())
	assert.Equal(t, []string{"cloud-init status"}, d.ran())
}

func TestDialSSHUnreachable(t *testing.T) {
	d, key := newFakeSSHD(t)
	d.unreachable = true
	_, err := DialSSH(context.Background(), fakeServerIP, key)
	assert.ErrorIs(t, err, errUnreachable)
}

func TestDialSSHRejectsAnUnparsableKey(t *testing.T) {
	d, _ := newFakeSSHD(t)
	_, err := DialSSH(context.Background(), fakeServerIP, []byte("not a key"))
	assert.Error(t, err)
	assert.NotErrorIs(t, err, errUnreachable)
	assert.Zero(t, d.dialCount(), "a bad key is rejected before connecting")
}

func TestDialSSHRefusedKey(t *testing.T) {
	d, key := newFakeSSHD(t)
	d.authorized = nil
	_, err := DialSSH(context.Background(), fakeServerIP, key)
	assert.ErrorContains(t, err, "unable to authenticate")
	assert.NotErrorIs(t, err, errUnreachable)
}

// A server that accepts the connection but never answers (still booting, or filtered)
// must cost the timeout, not hang, and must not leave the connection open.
func TestDialSSHHungHandshakeIsBoundedByContext(t *testing.T) {
	d, key := newFakeSSHD(t)
	d.hang = true

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := DialSSH(ctx, fakeServerIP, key)
		done <- err
	}()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(10 * time.Second):
		t.Fatal("DialSSH did not return when its context expired")
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	require.Len(t, d.conns, 2)
	assert.True(t, d.conns[0].out.closed, "the connection is closed, not leaked")
}

func TestDialSSHCancelledBeforeConnecting(t *testing.T) {
	_, key := newFakeSSHD(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DialSSH(ctx, fakeServerIP, key)
	assert.True(t, isContextErr(err), "got %v", err)
}

func TestAttemptConnectionReportsCloudInitStatus(t *testing.T) {
	d, key := newFakeSSHD(t, statusRunning)
	status, err := attemptConnection(context.Background(), fakeServerIP, key)
	require.NoError(t, err)
	assert.Equal(t, statusRunning, status.Status)
	assert.Equal(t, []string{"cloud-init status || true"}, d.ran(), "the disk check waits for cloud-init to finish")
}

func TestAttemptConnectionChecksTheDiskOnceProvisioned(t *testing.T) {
	d, key := newFakeSSHD(t, "done")
	d.dfOutput = "space 91%\ninodes 3%\n"
	status, err := attemptConnection(context.Background(), fakeServerIP, key)
	require.NoError(t, err)
	assert.Equal(t, "done", status.Status)

	ran := d.ran()
	require.Len(t, ran, 2, "the disk check reuses the connection of the status check")
	assert.True(t, strings.HasPrefix(ran[1], "df -P /home/"+SSHUsername), ran[1])
	assert.Equal(t, 1, d.dialCount())
}

func TestAttemptConnectionDistinguishesAuthFromUnreachable(t *testing.T) {
	d, key := newFakeSSHD(t)
	d.authorized = nil
	_, err := attemptConnection(context.Background(), fakeServerIP, key)
	assert.ErrorIs(t, err, errSSHAuth)

	d.unreachable = true
	_, err = attemptConnection(context.Background(), fakeServerIP, key)
	assert.ErrorIs(t, err, errUnreachable)
	assert.NotErrorIs(t, err, errSSHAuth)
}

func TestWaitForProvisionedWaitsForCloudInit(t *testing.T) {
	d, key := newFakeSSHD(t, statusRunning, statusRunning, "degraded done")
	h := &Hetzner{poll: time.Millisecond}
	assert.NoError(t, h.waitForProvisioned(boundedCtx(t), testServer(), key, false))
	assert.Equal(t, 3, cloudInitChecks(d))
}

func TestWaitForProvisionedRetriesWhileUnreachable(t *testing.T) {
	d, key := newFakeSSHD(t, "done")
	d.refuseFirst = 3
	h := &Hetzner{poll: time.Millisecond}
	assert.NoError(t, h.waitForProvisioned(boundedCtx(t), testServer(), key, false))
	assert.Equal(t, 4, d.dialCount())
}

func TestWaitForProvisionedFailsWhenCloudInitKeepsFailing(t *testing.T) {
	d, key := newFakeSSHD(t, statusError)
	h := &Hetzner{poll: time.Millisecond}
	err := h.waitForProvisioned(boundedCtx(t), testServer(), key, false)
	assert.ErrorIs(t, err, ErrProvisioningFailed)
	assert.Equal(t, cloudInitErrorThreshold, cloudInitChecks(d))
}

func TestWaitForProvisionedToleratesATransientCloudInitError(t *testing.T) {
	d, key := newFakeSSHD(t, statusError, statusError, statusRunning, statusError, statusError, "done")
	h := &Hetzner{poll: time.Millisecond}
	assert.NoError(t, h.waitForProvisioned(boundedCtx(t), testServer(), key, false))
	assert.Equal(t, 6, cloudInitChecks(d))
}

func TestWaitForProvisionedGivesUpOnARefusedKeyWhenAsked(t *testing.T) {
	d, key := newFakeSSHD(t)
	d.authorized = nil
	h := &Hetzner{poll: time.Millisecond}
	err := h.waitForProvisioned(boundedCtx(t), testServer(), key, true)
	assert.ErrorIs(t, err, ErrProvisioningFailed)
	assert.ErrorContains(t, err, "refused this machine's SSH key")
	assert.Equal(t, authFailureThreshold, d.dialCount())
}

// On a new server the key is the one just installed, so a refusal there is only
// sshd not being ready yet: keep waiting until the deadline.
func TestWaitForProvisionedKeepsWaitingOnARefusedKeyOnANewServer(t *testing.T) {
	d, key := newFakeSSHD(t)
	d.authorized = nil
	h := &Hetzner{poll: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := h.waitForProvisioned(ctx, testServer(), key, false)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Greater(t, d.dialCount(), authFailureThreshold)
}

func TestWaitForProvisionedTimeout(t *testing.T) {
	_, key := newFakeSSHD(t, statusRunning)
	h := &Hetzner{poll: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := h.waitForProvisioned(ctx, testServer(), key, false)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "waiting for server to be provisioned")
}
