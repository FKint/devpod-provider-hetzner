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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommandRunsOnTheServer(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	sshd, key := newFakeSSHD(t)

	var stdout, stderr bytes.Buffer
	err := cloud.hetzner(t).RunCommand(boundedCtx(t), machineName, key, "echo hello", strings.NewReader(""), &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, "hello\n", stdout.String())
	assert.Equal(t, []string{"echo hello"}, sshd.ran())
}

func TestRunCommandReportsAMissingServer(t *testing.T) {
	cloud := newFakeCloud()
	sshd, key := newFakeSSHD(t)

	err := cloud.hetzner(t).RunCommand(boundedCtx(t), machineName, key, "ls", nil, &bytes.Buffer{}, &bytes.Buffer{})

	assert.ErrorIs(t, err, ErrServerNotFound)
	assert.Zero(t, sshd.dialCount(), "there is nothing to connect to")
}

func TestRunCommandReportsAFailingLookup(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	cloud.fail(reqListServers)
	_, key := newFakeSSHD(t)

	err := cloud.hetzner(t).RunCommand(boundedCtx(t), machineName, key, "ls", nil, &bytes.Buffer{}, &bytes.Buffer{})

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrServerNotFound)
}

func TestRunCommandReportsConnectionFailures(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, statusRunning)
	sshd, key := newFakeSSHD(t)
	sshd.unreachable = true

	err := cloud.hetzner(t).RunCommand(boundedCtx(t), machineName, key, "ls", nil, &bytes.Buffer{}, &bytes.Buffer{})
	assert.ErrorContains(t, err, "create ssh client")

	err = cloud.hetzner(t).RunCommand(boundedCtx(t), machineName, []byte("not a key"), "ls", nil, &bytes.Buffer{}, &bytes.Buffer{})
	assert.ErrorContains(t, err, "create ssh client")
}
