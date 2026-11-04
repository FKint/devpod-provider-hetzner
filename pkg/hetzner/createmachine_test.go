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
	"encoding/base64"
	"testing"

	"github.com/loft-sh/devpod/pkg/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cryptoSsh "golang.org/x/crypto/ssh"
)

func TestCreateMachineCreatesTheMachineFromTheOptions(t *testing.T) {
	cloud := newFakeCloud()
	sshd, _ := newFakeSSHD(t, "done")
	opts := testOptions(t)

	// The machine folder holds the key pair BuildServerOptions uses; the fake sshd must accept it
	publicKey, err := ssh.GetPublicKeyBase(opts.MachineFolder)
	require.NoError(t, err)
	authorizedKey, err := base64.StdEncoding.DecodeString(publicKey)
	require.NoError(t, err)
	//nolint:dogsled // only the key is wanted
	sshd.authorized, _, _, _, err = cryptoSsh.ParseAuthorizedKey(authorizedKey)
	require.NoError(t, err)

	require.NoError(t, cloud.hetzner(t).CreateMachine(boundedCtx(t), opts, 10, testMachineOptions(t)))

	servers, volumes, keys := cloud.left()
	assert.Equal(t, []string{machineName}, servers)
	assert.Equal(t, []string{machineName}, volumes)
	assert.Len(t, keys, 1, "the key of the machine folder is uploaded")
}

// The key is uploaded before Create runs, so Create removes it when it refuses early.
func TestCreateMachineRemovesTheKeyWhenCreateRefusesEarly(t *testing.T) {
	cloud := newFakeCloud()
	machine := testMachineOptions(t)
	machine.SwapSizeBytes = int64(fakeServerDisk) << 30

	err := cloud.hetzner(t).CreateMachine(boundedCtx(t), testOptions(t), 10, machine)
	assert.ErrorIs(t, err, ErrSwapTooLarge)
	assert.Equal(t, 1, cloud.count("POST /ssh_keys"), "the key was uploaded first")
	assertNothingLeft(t, cloud)
}

func TestCreateMachineStopsAtTheFirstError(t *testing.T) {
	cloud := newFakeCloud()
	opts := testOptions(t)
	opts.Region = "nowhere"

	assert.ErrorIs(t, cloud.hetzner(t).CreateMachine(boundedCtx(t), opts, 10, testMachineOptions(t)), ErrUnknownRegion)
	assertNothingLeft(t, cloud)
	assert.Zero(t, cloud.count(reqCreateServer))
}
