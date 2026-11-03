/*
 * Copyright 2023 Simon Emms <simon@simonemms.com>
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
	"crypto/rand"
	"embed"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	cryptoSsh "golang.org/x/crypto/ssh"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/devpod/pkg/client"
	"github.com/loft-sh/devpod/pkg/ssh"
	"github.com/loft-sh/log"
	"sigs.k8s.io/yaml"
)

//go:embed cloud-config.yaml
var cloudConfig embed.FS

type cloudInit struct {
	Status string `json:"status"`
}

type Hetzner struct {
	client *hcloud.Client
}

func NewHetzner(token string) *Hetzner {
	return &Hetzner{
		client: hcloud.NewClient(hcloud.WithToken(token)),
	}
}

func (h *Hetzner) upsertPublicKey(ctx context.Context, publicKey, machineID string) (*hcloud.SSHKey, error) {
	fingerprint, err := generateSSHKeyFingerprint(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate fingerprint for public ssh key: %w", err)
	}

	sshKey, _, err := h.client.SSHKey.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, err
	}

	if sshKey == nil {
		name := sshKeyName(machineID, rand.Text())

		log.Default.Infof("Uploading SSH key: %s", name)

		// Upload the key
		uploadedSSHKey, _, err := h.client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
			Name:      name,
			PublicKey: publicKey,
			Labels: map[string]string{
				"type":         "devpod",
				labelMachineID: machineID,
			},
		})
		if err != nil {
			return nil, err
		}

		sshKey = uploadedSSHKey
	}

	return sshKey, nil
}

// sshKeyName builds a unique SSH key name from the machine ID. Only the name is
// truncated: the machineId label must keep the full ID so Delete can find the key.
func sshKeyName(machineID, random string) string {
	if len(machineID) > maxSSHKeyNameIDLength {
		machineID = machineID[:maxSSHKeyNameIDLength]
	}

	return fmt.Sprintf("%s-%s", machineID, strings.ToLower(random[:8]))
}

func (h *Hetzner) BuildServerOptions(
	ctx context.Context,
	opts *options.Options,
) (serverCreateOpts *hcloud.ServerCreateOpts, publicKeyStr *string, privateKey []byte, err error) {
	log.Default.Debugf("Machine folder path: %s", opts.MachineFolder)

	publicKeyBase, err := ssh.GetPublicKeyBase(opts.MachineFolder)
	if err != nil {
		return nil, nil, nil, err
	}

	publicKey, err := base64.StdEncoding.DecodeString(publicKeyBase)
	if err != nil {
		return nil, nil, nil, err
	}

	privateKey, err = ssh.GetPrivateKeyRawBase(opts.MachineFolder)
	if err != nil {
		return nil, nil, nil, err
	}

	location, _, err := h.client.Location.GetByName(ctx, opts.Region)
	if err != nil {
		return nil, nil, nil, err
	}
	if location == nil {
		return nil, nil, nil, ErrUnknownRegion
	}

	serverType, _, err := h.client.ServerType.GetByName(ctx, opts.MachineType)
	if err != nil {
		return nil, nil, nil, err
	}
	if serverType == nil {
		return nil, nil, nil, ErrUnknownMachineID
	}

	sshKey, err := h.upsertPublicKey(ctx, string(publicKey), opts.MachineID)
	if err != nil {
		return nil, nil, nil, err
	}

	arch := hcloud.ArchitectureX86
	if strings.HasPrefix(opts.MachineType, "cax") {
		// Machines starting "cax" are ARM64
		arch = hcloud.ArchitectureARM
	}
	image, _, err := h.client.Image.GetByNameAndArchitecture(ctx, opts.DiskImage, arch)
	if err != nil {
		return nil, nil, nil, err
	}
	if image == nil {
		return nil, nil, nil, ErrUnknownDiskImage
	}

	return &hcloud.ServerCreateOpts{
		Name:       opts.MachineID,
		Location:   location,
		ServerType: serverType,
		Image:      image,
		Labels: map[string]string{
			"type":         "devpod",
			labelMachineID: opts.MachineID,
		},
		SSHKeys: []*hcloud.SSHKey{
			sshKey,
		},
	}, new(string(publicKey)), privateKey, nil
}

func (h *Hetzner) Create(
	ctx context.Context,
	req *hcloud.ServerCreateOpts,
	diskSize int,
	publicKey string,
	privateKeyFile []byte,
) (err error) {
	log.Default.Info("Creating DevPod instance")

	// The server may already exist (e.g. "start" on a machine that is powered off or still booting)
	existing, err := h.GetByName(ctx, req.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		return h.startExisting(ctx, existing, privateKeyFile)
	}

	volume, err := h.volumeByName(ctx, req.Name)
	if err != nil {
		return err
	}

	createdVolume := false
	if volume == nil {
		// Create the volume as it doesn't exist
		log.Default.Info("Creating a new volume")

		result, _, err := h.client.Volume.Create(ctx, hcloud.VolumeCreateOpts{
			Location:  req.Location,
			Name:      req.Name,
			Size:      diskSize,
			Format:    new("ext4"),
			Automount: new(false),
			Labels:    req.Labels,
		})
		if err != nil {
			return err
		}

		volume = result.Volume
		createdVolume = true

		if err := h.waitForActions(ctx, append([]*hcloud.Action{result.Action}, result.NextActions...)...); err != nil {
			log.Default.Errorf("Error in volume creation action: %s", err)
			h.rollback(ctx, nil, volume)
			return err
		}

		log.Default.Info("Volume successfully created")
	}

	// Remove anything created by this call if it doesn't succeed. A volume that
	// already existed holds the user's data, so it is never removed here.
	var createdServer *hcloud.Server
	defer func() {
		if err == nil {
			return
		}
		var rollbackVolume *hcloud.Volume
		if createdVolume {
			rollbackVolume = volume
		}
		h.rollback(context.WithoutCancel(ctx), createdServer, rollbackVolume)
	}()

	// Generate the config init
	userData, err := generateUserData(publicKey, volume.ID)
	if err != nil {
		return err
	}
	// Add to server config
	req.UserData = userData.String()

	// Add volume to the server config
	req.Volumes = []*hcloud.Volume{
		{
			ID: volume.ID,
		},
	}

	// Create the server
	log.Default.Info("Creating a new server")
	server, _, err := h.client.Server.Create(ctx, *req)
	if err != nil {
		return err
	}
	createdServer = server.Server

	log.Default.Info("Server creation triggered")

	if err := h.waitForActions(ctx, append([]*hcloud.Action{server.Action}, server.NextActions...)...); err != nil {
		log.Default.Errorf("Error in server creation action: %s", err)
		return err
	}

	log.Default.Info("Server created - provisioning")

	return waitForProvisioned(ctx, server.Server, privateKeyFile)
}

// startExisting brings an already existing server to the provisioned state.
func (h *Hetzner) startExisting(ctx context.Context, server *hcloud.Server, privateKeyFile []byte) error {
	if server.Status == hcloud.ServerStatusOff {
		log.Default.Info("Server exists but is powered off - powering on")

		action, _, err := h.client.Server.Poweron(ctx, server)
		if err != nil {
			return fmt.Errorf("power on server: %w", err)
		}
		if err := h.waitForActions(ctx, action); err != nil {
			return err
		}
	} else {
		log.Default.Infof("Server already exists (status: %s)", server.Status)
	}

	return waitForProvisioned(ctx, server, privateKeyFile)
}

// rollback removes resources created by a failed Create. Failures are logged, not returned,
// so the original error is what the caller sees.
func (h *Hetzner) rollback(ctx context.Context, server *hcloud.Server, volume *hcloud.Volume) {
	if server != nil {
		log.Default.Warnf("Rolling back: deleting server %s", server.Name)

		result, _, err := h.client.Server.DeleteWithResult(ctx, server)
		if err == nil {
			err = h.waitForActions(ctx, result.Action)
		}
		if err != nil {
			log.Default.Errorf("Unable to delete server %s, please remove it manually: %v", server.Name, err)
			// The volume can't be removed while attached to the server
			return
		}
	}

	if volume != nil {
		log.Default.Warnf("Rolling back: deleting volume %s", volume.Name)

		if _, err := h.client.Volume.Delete(ctx, volume); err != nil {
			log.Default.Errorf("Unable to delete volume %s, please remove it manually: %v", volume.Name, err)
		}
	}
}

// waitForProvisioned polls cloud-init over SSH until it reports success, fails or the timeout expires.
func waitForProvisioned(ctx context.Context, server *hcloud.Server, privateKeyFile []byte) error {
	ctx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()

	for attempt := 1; ; attempt++ {
		log.Default.Debugf("Provisioning check %d", attempt)

		status := attemptConnection(ctx, server.PublicNet.IPv4.IP.String(), privateKeyFile)
		switch {
		case status == nil:
			// Not reachable yet
		case isCloudInitDone(status.Status):
			log.Default.Info("Server provisioned")
			return nil
		case isCloudInitFailed(status.Status):
			return fmt.Errorf("%w: cloud-init status %q", ErrProvisioningFailed, status.Status)
		default:
			log.Default.Debug("Server not yet provisioned")
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for server to be provisioned: %w", ctx.Err())
		case <-time.After(provisionPoll):
		}
	}
}

func isCloudInitDone(status string) bool {
	// "degraded done" means cloud-init finished with recoverable errors
	return status == "done" || status == "degraded done"
}

func isCloudInitFailed(status string) bool {
	return status == "error" || status == "disabled"
}

// Delete removes the server, then its volume and SSH keys.
func (h *Hetzner) Delete(ctx context.Context, name string) error {
	// The server goes first: a volume can't be deleted while attached, and
	// deleting the server detaches it without hot-unplugging a mounted disk.
	server, err := h.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if server != nil {
		result, _, err := h.client.Server.DeleteWithResult(ctx, server)
		if err != nil {
			return err
		}
		if err := h.waitForActions(ctx, result.Action); err != nil {
			return err
		}
	}

	if err := h.deleteVolume(ctx, name); err != nil {
		return err
	}

	// Delete SSH keys
	keys, _, err := h.client.SSHKey.List(ctx, hcloud.SSHKeyListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("%s=%s", labelMachineID, name),
		},
	})
	if err != nil {
		return err
	}

	for _, k := range keys {
		log.Default.Infof("Deleting SSH key: %s", k.Name)
		if _, err := h.client.SSHKey.Delete(ctx, k); err != nil {
			return err
		}
	}

	return nil
}

func (h *Hetzner) GetByName(ctx context.Context, name string) (*hcloud.Server, error) {
	servers, _, err := h.client.Server.List(ctx, hcloud.ServerListOpts{Name: name})
	if err != nil {
		return nil, err
	}

	serverLength := len(servers)
	if serverLength > 1 {
		return nil, ErrMultipleServersFound(name)
	}
	if serverLength == 0 {
		return nil, nil
	}

	return servers[0], nil
}

func (h *Hetzner) Init(ctx context.Context) error {
	_, _, err := h.client.Server.List(ctx, hcloud.ServerListOpts{})
	if err != nil {
		return err
	}
	return nil
}

func (h *Hetzner) Status(ctx context.Context, name string) (client.Status, error) {
	server, err := h.GetByName(ctx, name)
	if err != nil {
		return client.StatusNotFound, err
	}
	if server == nil {
		// No server - check the volume
		volume, err := h.volumeByName(ctx, name)
		if err != nil {
			return client.StatusNotFound, err
		} else if volume != nil {
			return client.StatusStopped, nil
		}

		return client.StatusNotFound, nil
	}

	switch server.Status {
	case hcloud.ServerStatusRunning:
		return client.StatusRunning, nil
	case hcloud.ServerStatusOff:
		return client.StatusStopped, nil
	default:
		// Starting, stopping, migrating, etc.
		return client.StatusBusy, nil
	}
}

// WaitForStatus polls until the machine reaches the wanted status or the timeout expires.
func (h *Hetzner) WaitForStatus(ctx context.Context, name string, want client.Status) error {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	for {
		status, err := h.Status(ctx, name)
		if err != nil {
			return fmt.Errorf("retrieving server status: %w", err)
		} else if status == want {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for status %q (currently %q): %w", want, status, ctx.Err())
		case <-time.After(statusPoll):
		}
	}
}

func (h *Hetzner) Stop(ctx context.Context, name string) error {
	server, err := h.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if server == nil {
		return nil
	}

	result, _, err := h.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return err
	}

	return h.waitForActions(ctx, result.Action)
}

func (h *Hetzner) deleteVolume(ctx context.Context, name string) error {
	volume, err := h.volumeByName(ctx, name)
	if err != nil {
		return err
	} else if volume == nil {
		return nil
	}

	if volume.Server != nil {
		// Detach volume
		action, _, err := h.client.Volume.Detach(ctx, volume)
		if err != nil {
			return fmt.Errorf("detach volume: %w", err)
		}

		if err := h.waitForActions(ctx, action); err != nil {
			log.Default.Errorf("Error in volume detach action: %s, %s", action.Command, err)
			return err
		}

		// Wait until the volume is detached
		waitCtx, cancel := context.WithTimeout(ctx, actionTimeout)
		defer cancel()

		for volume.Server != nil {
			select {
			case <-waitCtx.Done():
				return fmt.Errorf("waiting for volume to detach: %w", waitCtx.Err())
			case <-time.After(time.Second):
			}

			volume, err = h.volumeByName(waitCtx, name)
			if err != nil {
				return err
			} else if volume == nil {
				return nil
			}
		}
	}

	if _, err := h.client.Volume.Delete(ctx, volume); err != nil {
		return fmt.Errorf("delete volume: %w", err)
	}

	return nil
}

func (h *Hetzner) volumeByName(ctx context.Context, name string) (*hcloud.Volume, error) {
	volumes, _, err := h.client.Volume.List(ctx, hcloud.VolumeListOpts{
		Name: name,
	})
	if err != nil {
		return nil, err
	}

	volLen := len(volumes)
	if volLen > 1 {
		return nil, ErrMultipleVolumesFound(name)
	}
	if volLen == 0 {
		return nil, nil
	}

	return volumes[0], nil
}

func attemptConnection(ctx context.Context, addr string, privateKeyFile []byte) *cloudInit {
	log.Default.Debug("Checking server provision status")

	// Check the server is provisioned - this runs "ssh user@path cloud-init status"
	sshClient, err := ssh.NewSSHClient(SSHUsername, fmt.Sprintf("%s:%d", addr, SSHPort), privateKeyFile)
	if err != nil {
		log.Default.Warnf("Unable to connect to server: %v", err)
		return nil
	}
	defer func() {
		if err := sshClient.Close(); err != nil {
			log.Default.Debugf("Error closing SSH connection: %v", err)
		}
	}()

	buf := new(bytes.Buffer)
	if err := ssh.Run(ctx, sshClient, "cloud-init status || true", &bytes.Buffer{}, buf, &bytes.Buffer{}, nil); err != nil {
		log.Default.Errorf("Error retrieving cloud-init status, %v", err)
		return nil
	}

	status, err := parseCloudInitStatus(buf.Bytes())
	if err != nil {
		log.Default.Errorf("Unable to parse cloud-init YAML: %v", err)
		return nil
	}

	return status
}

func parseCloudInitStatus(out []byte) (*cloudInit, error) {
	var status cloudInit
	if err := yaml.Unmarshal(out, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

func generateSSHKeyFingerprint(publicKey string) (string, error) {
	//nolint:dogsled // correct assignment
	pk, _, _, _, err := cryptoSsh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", err
	}

	return cryptoSsh.FingerprintLegacyMD5(pk), nil
}

func generateUserData(publicKey string, volumeID int64) (*bytes.Buffer, error) {
	t, err := template.New("cloud-config.yaml").ParseFS(cloudConfig, "cloud-config.yaml")
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := t.Execute(buf, map[string]string{
		"PublicKey": strings.TrimSuffix(publicKey, "\n"),
		"VolumeID":  strconv.FormatInt(volumeID, 10),
		"Username":  SSHUsername,
	}); err != nil {
		return nil, err
	}

	return buf, nil
}

// waitForActions blocks until all actions succeed, one fails, or the timeout/context expires.
func (h *Hetzner) waitForActions(ctx context.Context, actions ...*hcloud.Action) error {
	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	return h.client.Action.WaitFor(ctx, actions...)
}
