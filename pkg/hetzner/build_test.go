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
	"os"
	"path/filepath"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/loft-sh/devpod/pkg/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cryptoSsh "golang.org/x/crypto/ssh"
)

func testOptions(t *testing.T) *options.Options {
	t.Helper()

	return &options.Options{
		MachineID:     machineName,
		MachineFolder: t.TempDir(),
		Region:        fakeRegion,
		MachineType:   fakeMachineType,
		DiskImage:     fakeImage,
	}
}

func TestBuildServerOptions(t *testing.T) {
	cloud := newFakeCloud()
	opts := testOptions(t)

	req, publicKey, privateKey, err := cloud.hetzner(t).BuildServerOptions(context.Background(), opts)
	require.NoError(t, err)

	assert.Equal(t, machineName, req.Name)
	assert.Equal(t, fakeRegion, req.Location.Name)
	assert.Equal(t, fakeMachineType, req.ServerType.Name)
	assert.Equal(t, fakeImage, req.Image.Name)
	assert.Equal(t, machineLabels(machineName), req.Labels)

	// The uploaded key is the machine's own key, labelled so Delete can find it
	require.Len(t, req.SSHKeys, 1)
	assert.Contains(t, req.SSHKeys[0].Name, "machine-")
	assert.Equal(t, machineLabels(machineName), req.SSHKeys[0].Labels)
	fp, err := generateSSHKeyFingerprint(publicKey)
	require.NoError(t, err)
	assert.Equal(t, fp, req.SSHKeys[0].Fingerprint)

	// ...and the private key returned is the one for it
	signer, err := cryptoSsh.ParsePrivateKey(privateKey)
	require.NoError(t, err)
	assert.Equal(t, cryptoSsh.FingerprintLegacyMD5(signer.PublicKey()), fp)

	// The key pair is stored in the machine folder
	_, err = os.Stat(filepath.Join(opts.MachineFolder, ssh.DevPodSSHPrivateKeyFile))
	assert.NoError(t, err)
}

func TestBuildServerOptionsReusesTheUploadedKey(t *testing.T) {
	cloud := newFakeCloud()
	opts := testOptions(t)
	h := cloud.hetzner(t)

	first, _, _, err := h.BuildServerOptions(context.Background(), opts)
	require.NoError(t, err)
	second, _, _, err := h.BuildServerOptions(context.Background(), opts)
	require.NoError(t, err)

	assert.Equal(t, 1, cloud.count("POST /ssh_keys"), "start/create on an existing machine must not pile up keys")
	assert.Equal(t, first.SSHKeys[0].ID, second.SSHKeys[0].ID)
}

func TestBuildServerOptionsRejectsUnknownNames(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*options.Options)
		unknown string
		want    error
	}{
		{"region", func(o *options.Options) { o.Region = "mars1" }, "", ErrUnknownRegion},
		{"machine type", func(o *options.Options) { o.MachineType = "cx999" }, "", ErrUnknownMachineType},
		{"disk image", func(o *options.Options) { o.DiskImage = "nope" }, "", ErrUnknownDiskImage},
		{"region (API says none)", func(*options.Options) {}, "location", ErrUnknownRegion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloud := newFakeCloud()
			if tt.unknown != "" {
				cloud.unknown[tt.unknown] = true
			}
			opts := testOptions(t)
			tt.mutate(opts)

			req, _, _, err := cloud.hetzner(t).BuildServerOptions(context.Background(), opts)
			assert.ErrorIs(t, err, tt.want)
			assert.Nil(t, req)
		})
	}
}

// Region, machine type and image are checked before the key is uploaded, so a typo in
// them leaves nothing behind in the account.
func TestBuildServerOptionsUploadsNoKeyForAnUnknownRegionTypeOrImage(t *testing.T) {
	for _, mutate := range []func(*options.Options){
		func(o *options.Options) { o.Region = "mars1" },
		func(o *options.Options) { o.MachineType = "cx999" },
		func(o *options.Options) { o.DiskImage = "nope" },
	} {
		cloud := newFakeCloud()
		opts := testOptions(t)
		mutate(opts)
		_, _, _, err := cloud.hetzner(t).BuildServerOptions(context.Background(), opts)
		require.Error(t, err)
		assert.Zero(t, cloud.count("POST /ssh_keys"))
	}
}

func TestBuildServerOptionsReportsAPIErrors(t *testing.T) {
	for _, failing := range []string{"GET /locations", "GET /server_types", "GET /images", "GET /ssh_keys", "POST /ssh_keys"} {
		t.Run(failing, func(t *testing.T) {
			cloud := newFakeCloud()
			cloud.fail(failing)
			_, _, _, err := cloud.hetzner(t).BuildServerOptions(context.Background(), testOptions(t))
			assert.ErrorContains(t, err, "injected failure")
		})
	}
}

func TestBuildServerOptionsNeedsAUsableMachineFolder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	opts := testOptions(t)
	opts.MachineFolder = filepath.Join(file, "sub") // a folder below a regular file can't be created

	//nolint:dogsled // only the error is wanted
	_, _, _, err := newFakeCloud().hetzner(t).BuildServerOptions(context.Background(), opts)
	assert.Error(t, err)
}

// The public key is read before the private key, so it may be fine while the private key is not
func TestBuildServerOptionsReportsAnUnreadablePrivateKey(t *testing.T) {
	opts := testOptions(t)
	require.NoError(t, os.WriteFile(filepath.Join(opts.MachineFolder, ssh.DevPodSSHPublicKeyFile), []byte("ssh-ed25519 AAAA test"), 0o600))
	// A directory where the private key should be: it exists, but can't be read
	require.NoError(t, os.Mkdir(filepath.Join(opts.MachineFolder, ssh.DevPodSSHPrivateKeyFile), 0o700))

	cloud := newFakeCloud()
	//nolint:dogsled // only the error is wanted
	_, _, _, err := cloud.hetzner(t).BuildServerOptions(context.Background(), opts)
	assert.ErrorContains(t, err, "read private ssh key")
	assert.Zero(t, cloud.count("POST /ssh_keys"), "no key is uploaded without a private key")
}

func TestUpsertPublicKeyRejectsAnInvalidKey(t *testing.T) {
	cloud := newFakeCloud()
	_, err := cloud.hetzner(t).upsertPublicKey(context.Background(), "garbage", machineName)
	assert.ErrorContains(t, err, "fingerprint")
	assert.Zero(t, cloud.count("POST /ssh_keys"))
}

func TestUpsertPublicKeyUploadsOncePerKey(t *testing.T) {
	cloud := newFakeCloud()
	_, authorized := newSSHKeyPair(t)
	h := cloud.hetzner(t)

	a, err := h.upsertPublicKey(context.Background(), authorized, machineName)
	require.NoError(t, err)
	b, err := h.upsertPublicKey(context.Background(), authorized, machineName)
	require.NoError(t, err)
	assert.Equal(t, a.ID, b.ID)
	assert.Equal(t, 1, cloud.count("POST /ssh_keys"))
}
