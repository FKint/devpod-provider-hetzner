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

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// The key is uploaded before Create, so even the very first lookup failing must not leave it behind
func TestCreateRemovesTheKeyWhenTheFirstLookupFails(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedKey(machineName)
	cloud.fail(reqListServers)
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	assertNothingLeft(t, cloud)
}

// Starting an existing server never removes anything, not even when its lookup works but the start fails
func TestCreateKeepsTheKeyWhenStartingAnExistingServerFails(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedServer(machineName, "off")
	cloud.seedKey(machineName)
	cloud.failAction(reqPoweron)
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	servers, _, keys := cloud.left()
	assert.Len(t, servers, 1)
	assert.Len(t, keys, 1)
}

// The key goes even when the server can't be removed, so only the server is left to clean up by hand
func TestRollbackRemovesTheKeyWhenTheServerCannotBeDeleted(t *testing.T) {
	cloud := newFakeCloud()
	cloud.seedKey(machineName)
	cloud.loseResponse(reqCreateServer)
	cloud.fail(reqDeleteServer)
	_, key := newFakeSSHD(t)

	assert.Error(t, createMachine(t, cloud, key, 10))
	servers, _, keys := cloud.left()
	assert.Len(t, servers, 1, "the server could not be deleted")
	assert.Empty(t, keys)
}

// A server found under the name after a failed create is only ours if this attempt made it
func TestCreateServerAdoptsOnlyItsOwnServerAfterALostResponse(t *testing.T) {
	t.Run("server of another run", func(t *testing.T) {
		cloud := newFakeCloud()
		cloud.seedServer(machineName, statusRunning)
		cloud.fail(reqCreateServer)
		var created *hcloud.Server

		assert.Error(t, cloud.hetzner(t).createServer(context.Background(), testRequest(), &created))
		assert.Nil(t, created, "a rollback would delete the other run's server")
	})

	t.Run("server of this attempt", func(t *testing.T) {
		cloud := newFakeCloud()
		cloud.loseResponse(reqCreateServer)
		var created *hcloud.Server

		assert.Error(t, cloud.hetzner(t).createServer(context.Background(), testRequest(), &created))
		require.NotNil(t, created)
		assert.Equal(t, machineName, created.Name)
	})
}

func TestUpsertPublicKeyRefusesAKeyOfAnotherMachine(t *testing.T) {
	fingerprint, err := generateSSHKeyFingerprint(testPublicKey)
	require.NoError(t, err)

	tests := []struct {
		name    string
		labels  map[string]string
		wantErr bool
	}{
		{"own key", machineLabels(machineName), false},
		{"another machine's key", machineLabels(otherName), true},
		{"unlabelled key", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloud := newFakeCloud()
			cloud.mu.Lock()
			k := &cloudKey{ID: cloud.id(), Name: "existing", Fingerprint: fingerprint, Labels: tt.labels}
			cloud.keys[k.ID] = k
			cloud.mu.Unlock()

			got, err := cloud.hetzner(t).upsertPublicKey(context.Background(), testPublicKey, machineName)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrSSHKeyInUse)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, k.ID, got.ID, "the key is reused, not uploaded again")
		})
	}
}

// The comment of a key is free text: it must not be able to break the quoted YAML scalar
func TestGenerateUserDataDropsTheKeyComment(t *testing.T) {
	buf, err := generateUserData(testPublicKeyData+" evil\" \\ comment: [\n", 1, defaultMachine(t))
	require.NoError(t, err)

	assert.NotContains(t, buf.String(), "evil")
	assert.Contains(t, buf.String(), `- "`+testPublicKeyData+`"`)
	assert.NoError(t, yaml.Unmarshal(buf.Bytes(), &map[string]any{}))

	_, err = generateUserData("not a key", 1, defaultMachine(t))
	assert.Error(t, err)
}
