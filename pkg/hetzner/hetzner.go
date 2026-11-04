/*
 * Copyright 2023 Simon Emms <simon@simonemms.com>
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
	"crypto/rand"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os/exec"
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

	// Zero values mean the defaults; tests shorten them.
	shutdownPoll    time.Duration
	shutdownTimeout time.Duration
	// runLocal runs a command on the machine we are running on (in-VM stop only).
	runLocal func(ctx context.Context, name string, args ...string) error
	// detachPoll is how often to check whether a volume was released; statusPoll if zero
	detachPoll time.Duration
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

	// A volume that belonged to the server just deleted by a stop may take a moment to be released
	volume, err := h.waitForVolumeDetached(ctx, req.Name)
	if err != nil {
		return err
	}

	// Remove anything created by this call if it doesn't succeed. A volume that
	// already existed holds the user's data, so it is never removed here. The SSH
	// key is uploaded before Create, but is re-uploaded on every create/start.
	var createdServer *hcloud.Server
	var createdVolume *hcloud.Volume
	defer func() {
		if err != nil {
			// The context may be what failed (e.g. Ctrl-C), so don't reuse it
			h.rollback(context.WithoutCancel(ctx), req.Name, createdServer, createdVolume)
		}
	}()

	if volume == nil {
		// Only a new volume uses the size, so only then does the minimum apply
		if diskSize < options.MinDiskSizeGB {
			return fmt.Errorf("%w: need at least %dGB, got %d", ErrDiskTooSmall, options.MinDiskSizeGB, diskSize)
		}

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
		createdVolume = volume

		if err := h.waitForActions(ctx, append([]*hcloud.Action{result.Action}, result.NextActions...)...); err != nil {
			log.Default.Errorf("Error in volume creation action: %s", err)
			return err
		}

		log.Default.Info("Volume successfully created")
	}

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
	if server.Server != nil {
		// Track it even when err is set: the API may have created it before failing
		createdServer = server.Server
	}
	if err != nil {
		return err
	}

	log.Default.Info("Server creation triggered")

	if err := h.waitForActions(ctx, append([]*hcloud.Action{server.Action}, server.NextActions...)...); err != nil {
		log.Default.Errorf("Error in server creation action: %s", err)
		return err
	}

	log.Default.Info("Server created - provisioning")

	return waitForProvisioned(ctx, server.Server, privateKeyFile, false)
}

// startExisting brings an already existing server to the provisioned state.
// It never deletes the server: if it can't be provisioned, the user is told to
// run delete, rather than risk removing something the user is still using.
func (h *Hetzner) startExisting(ctx context.Context, server *hcloud.Server, privateKeyFile []byte) error {
	switch server.Status {
	case hcloud.ServerStatusOff:
		log.Default.Info("Server exists but is powered off - powering on")

		action, _, err := h.client.Server.Poweron(ctx, server)
		if err != nil {
			return fmt.Errorf("power on server: %w", err)
		}
		if err := h.waitForActions(ctx, action); err != nil {
			return err
		}
	case hcloud.ServerStatusRunning, hcloud.ServerStatusStarting, hcloud.ServerStatusInitializing:
		log.Default.Infof("Server already exists (status: %s)", server.Status)
	default:
		// Stopping, deleting, migrating, etc. - nothing sensible to wait for
		return fmt.Errorf("%w: server %s is %s, try again shortly", ErrServerBusy, server.Name, server.Status)
	}

	if err := waitForProvisioned(ctx, server, privateKeyFile, true); err != nil {
		return fmt.Errorf(
			"%w (the existing server was left in place; if this machine's SSH key was regenerated it can't be reached. "+
				"Running delete removes the server AND its volume, including any data on it)",
			err,
		)
	}

	return nil
}

// rollback removes resources created by a failed Create. Failures are logged, not returned,
// so the original error is what the caller sees.
func (h *Hetzner) rollback(ctx context.Context, name string, server *hcloud.Server, volume *hcloud.Volume) {
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

		// Detaches first if needed and waits for the detach to settle
		if err := h.deleteVolume(ctx, volume.Name); err != nil {
			log.Default.Errorf("Unable to delete volume %s, please remove it manually: %v", volume.Name, err)
		}
	}

	if err := h.deleteSSHKeys(ctx, name); err != nil {
		log.Default.Errorf("Unable to delete SSH keys for %s: %v", name, err)
	}
}

// waitForProvisioned polls cloud-init over SSH until it reports success, fails or the timeout expires.
// With failOnAuth, repeated authentication refusals are treated as an error: the server
// is up but doesn't accept this machine's key (e.g. the key pair was regenerated).
func waitForProvisioned(ctx context.Context, server *hcloud.Server, privateKeyFile []byte, failOnAuth bool) error {
	ctx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()

	errorCount, authCount := 0, 0
	for attempt := 1; ; attempt++ {
		log.Default.Debugf("Provisioning check %d", attempt)

		status, err := attemptConnection(ctx, server.PublicNet.IPv4.IP.String(), privateKeyFile)
		switch {
		case errors.Is(err, errSSHAuth):
			authCount++
			if failOnAuth && authCount >= authFailureThreshold {
				return fmt.Errorf("%w: server %s refused this machine's SSH key", ErrProvisioningFailed, server.Name)
			}
		case err != nil:
			// Not reachable yet, or a transient failure. Only answers from cloud-init
			// count towards the consecutive "error" reports, so leave the count alone.
			authCount = 0
		case isCloudInitDone(status.Status):
			log.Default.Info("Server provisioned")
			return nil
		case isCloudInitFailed(status.Status):
			authCount = 0
			// Require the failure to persist so a transient report doesn't destroy a server
			errorCount++
			if errorCount >= cloudInitErrorThreshold {
				return fmt.Errorf("%w: cloud-init status %q", ErrProvisioningFailed, status.Status)
			}
		default:
			authCount, errorCount = 0, 0
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
	return status == "error"
}

// Delete removes the server, its volume and then its SSH keys.
func (h *Hetzner) Delete(ctx context.Context, name string) error {
	// The server goes before the volume: a volume can't be deleted while attached,
	// and deleting the server detaches it without hot-unplugging a mounted disk.
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

	// Keys last: until here the machine is intact, and every step is idempotent so a
	// failed delete (which returns an error to the caller) can simply be retried.
	return h.deleteSSHKeys(ctx, name)
}

func (h *Hetzner) deleteSSHKeys(ctx context.Context, name string) error {
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
		volume, live, err := h.volumeAndHolder(ctx, name)
		if err != nil {
			return client.StatusNotFound, err
		} else if volume == nil {
			return client.StatusNotFound, nil
		}

		// The server delete finishes before its volume is released. Until then the
		// machine can't be started (the volume would still be attached), so it isn't stopped yet.
		if volume.Server != nil && live == nil {
			return client.StatusBusy, nil
		}

		return client.StatusStopped, nil
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
// Transient API errors are retried until the deadline.
func (h *Hetzner) WaitForStatus(ctx context.Context, name string, want client.Status) error {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	var last client.Status
	var lastErr error
	for {
		status, err := h.Status(ctx, name)
		switch {
		case err != nil:
			lastErr = err
			log.Default.Warnf("Error retrieving server status, retrying: %v", err)
		case status == want:
			return nil
		default:
			lastErr = nil
			last = status
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("waiting for status %q: %w", want, lastErr)
			}
			return fmt.Errorf("waiting for status %q (currently %q): %w", want, last, ctx.Err())
		case <-time.After(statusPoll):
		}
	}
}

// Stop stops the machine by deleting its server; the volume keeps the data.
//
// Deleting a server is a power cut, so first the workloads are given a chance to
// write out their state. How depends on where we run:
//   - from a client (fromMachine false): ask the server to shut down (ACPI) and wait
//     for it to be off, so systemd stops the containers and flushes the disks;
//   - on the machine itself (fromMachine true, the inactivity shutdown): an ACPI
//     shutdown would kill this process before it could delete the server and leave
//     an "off" server that is still billed. So stop the containers and sync
//     ourselves, then delete.
//
// Neither preparation is fatal: the stop must always end with the server deleted.
func (h *Hetzner) Stop(ctx context.Context, name string, fromMachine bool) error {
	server, err := h.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if server == nil {
		return nil
	}

	if fromMachine {
		h.quiesceLocal(ctx)
	} else {
		h.shutdownGracefully(ctx, server)
	}

	result, _, err := h.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return err
	}

	return h.waitForActions(ctx, result.Action)
}

// shutdownGracefully asks the server to power off and waits until it has. It only
// warns on failure, so the caller can still delete the server.
func (h *Hetzner) shutdownGracefully(ctx context.Context, server *hcloud.Server) {
	if server.Status != hcloud.ServerStatusRunning {
		return
	}

	log.Default.Infof("Shutting down server %s", server.Name)
	action, _, err := h.client.Server.Shutdown(ctx, server)
	if err == nil {
		err = h.waitForActions(ctx, action)
	}
	if err != nil {
		log.Default.Warnf("Graceful shutdown failed, deleting the server anyway: %v", err)
		return
	}

	timeout, poll := h.shutdownTimeout, h.shutdownPoll
	if timeout == 0 {
		timeout = shutdownTimeout
	}
	if poll == 0 {
		poll = statusPoll
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		current, _, err := h.client.Server.GetByID(ctx, server.ID)
		switch {
		case err == nil && (current == nil || current.Status == hcloud.ServerStatusOff):
			return
		case err != nil && ctx.Err() == nil:
			log.Default.Warnf("Error retrieving server status, retrying: %v", err)
		}

		select {
		case <-ctx.Done():
			log.Default.Warnf("Server did not power off within %s, deleting it anyway", timeout)
			return
		case <-time.After(poll):
		}
	}
}

// quiesceLocal stops the running containers and flushes the disks of the machine
// we are running on. It only warns on failure.
func (h *Hetzner) quiesceLocal(ctx context.Context) {
	run := h.runLocal
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) error {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}

	// live-restore keeps containers running when dockerd stops, so they have to be
	// stopped explicitly; docker stop handles several containers concurrently.
	log.Default.Info("Stopping containers")
	stopAll := fmt.Sprintf("ids=$(docker ps -q); [ -z \"$ids\" ] || docker stop -t %d $ids", containerStopGrace)
	stopCtx, cancelStop := context.WithTimeout(ctx, localStopTimeout)
	defer cancelStop()
	if err := run(stopCtx, "sh", "-c", stopAll); err != nil {
		log.Default.Warnf("Could not stop the containers, continuing: %v", err)
	}
	// Its own budget: a slow container stop must not leave sync with an expired context
	syncCtx, cancelSync := context.WithTimeout(ctx, localStopTimeout)
	defer cancelSync()
	if err := run(syncCtx, "sync"); err != nil {
		log.Default.Warnf("Could not sync the disks, continuing: %v", err)
	}
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
	}

	// Make sure the detach has settled (also covers a server delete that is still releasing it)
	waitCtx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	for {
		volume, err = h.volumeByName(waitCtx, name)
		if err != nil {
			return err
		} else if volume == nil {
			return nil
		} else if volume.Server == nil {
			break
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("waiting for volume to detach: %w", waitCtx.Err())
		case <-time.After(time.Second):
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

// attemptConnection returns the cloud-init status, or an error if it couldn't be retrieved
// (errSSHAuth if the server refused our key).
func attemptConnection(ctx context.Context, addr string, privateKeyFile []byte) (*cloudInit, error) {
	log.Default.Debug("Checking server provision status")

	target := fmt.Sprintf("%s:%d", addr, SSHPort)

	// Bound the whole attempt (handshake and command) so a hung sshd can't outlive the provisioning deadline
	ctx, cancel := context.WithTimeout(ctx, sshAttemptTimeout)
	defer cancel()

	// ssh.NewSSHClient dials without a timeout or context, so probe the port first.
	// A server that is still booting (or filtered) then costs seconds, not minutes.
	conn, err := (&net.Dialer{Timeout: sshDialTimeout}).DialContext(ctx, "tcp", target)
	if err != nil {
		log.Default.Debugf("Server not reachable yet: %v", err)
		return nil, err
	}
	_ = conn.Close()

	// Check the server is provisioned - this runs "ssh user@path cloud-init status"
	sshClient, err := newSSHClient(ctx, target, privateKeyFile)
	if err != nil {
		log.Default.Warnf("Unable to connect to server: %v", err)
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("%w: %w", errSSHAuth, err)
		}
		return nil, err
	}
	defer func() {
		if err := sshClient.Close(); err != nil {
			log.Default.Debugf("Error closing SSH connection: %v", err)
		}
	}()

	buf := new(bytes.Buffer)
	if err := ssh.Run(ctx, sshClient, "cloud-init status || true", &bytes.Buffer{}, buf, &bytes.Buffer{}, nil); err != nil {
		log.Default.Errorf("Error retrieving cloud-init status, %v", err)
		return nil, err
	}

	status, err := parseCloudInitStatus(buf.Bytes())
	if err != nil {
		log.Default.Errorf("Unable to parse cloud-init YAML: %v", err)
		return nil, err
	}

	return status, nil
}

// newSSHClient wraps ssh.NewSSHClient, which takes no context or timeout, so a
// stalled handshake is abandoned when ctx ends instead of blocking forever.
// The abandoned goroutine (and its socket) lives until the handshake returns or the OS
// gives up, which is bounded by the one-attempt-per-poll cadence and the provisioning timeout.
func newSSHClient(ctx context.Context, target string, privateKey []byte) (*cryptoSsh.Client, error) {
	type result struct {
		client *cryptoSsh.Client
		err    error
	}

	ch := make(chan result, 1)
	go func() {
		c, err := ssh.NewSSHClient(SSHUsername, target, privateKey)
		ch <- result{c, err}
	}()

	select {
	case r := <-ch:
		return r.client, r.err
	case <-ctx.Done():
		// Close the connection if the handshake eventually completes
		go func() {
			if r := <-ch; r.client != nil {
				_ = r.client.Close()
			}
		}()

		return nil, ctx.Err()
	}
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
