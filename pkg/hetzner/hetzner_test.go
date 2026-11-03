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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"
)

func TestFingerPrintGenerate(t *testing.T) {
	tests := []struct {
		Name        string
		PublicKey   string
		Fingerprint string
		Error       error
	}{
		{
			Name: "rsa-1",
			//nolint
			PublicKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDVEnA5bsxU1ltrt9mPho/JrVeMS17sI9GjIeNCLcb2bIFTzZ6I8d+hFddgmHFItgLJLJWUYDIHjhE0yB6zLKVkDmeQ/T4Qy2UaV2x8O+KQa+7Chl8DaTfnr/0b8flaFG9VSLJKA/QJ/Sl07oCbRQt3l9bHXvVMux0VTGavEjpKwtFFtWkDx/vDxJoFsA+oMkGaF2AP2+jIc3WCATaprllUxI42pav52m065fpPEvMfK8LJ3L6t5IOa49LieoNPz23s5GOsN66E6kmNuuWQ/HH7I0vPovoeHqizX9CkHTdTYuI87Je39yEjVliMQurEUouHlZU075P06SBYGnObp9yp",
			Fingerprint: "99:e0:3a:b1:44:4a:7b:d1:6e:d7:61:a1:5f:f8:ec:6c",
		},
		{
			Name: "rsa-2",
			//nolint
			PublicKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDEAIu+Kqb3/3Lju+6r4DG7Vj36FtCf98wkWAcJECdvOde9QvBWLNC3butZZDUdu85ceQ0gRQrLXhLO8hwmf9ByRfUbsAiPR/xEMBKrYnHdaZEjwQMELGeoYpm3xQtcKHI5jRBdrR6jd0GLjwev8EDIJYmXF0Mu5GYR1aTadkKQBEPv52XcJgVS17HxI+L5s44xoqUedLUPBR2toj3ga7awzVDBRhlJRrShvmOso0AuOxRm1IfjtA1bsSgov2041v92d/xHURCfCLc6Nu/TEhKgx6DZk4flslMcRUdT5z/HeWfBtrjl0tTrJ6fIHffi/v9MsXXwnKe6dhUn5Ey10brN",
			Fingerprint: "20:98:ed:f5:86:af:db:0e:ea:99:97:12:88:ce:22:3b",
		},
		{
			Name:        "ed25519-1",
			PublicKey:   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMYMPf45N2zLPaI4SOxE4QJH/f4jhaLt7bSk75RVoIOA vscode@8422b61228f0",
			Fingerprint: "d4:dd:bf:79:27:15:d2:36:d1:13:60:79:6a:86:d8:7a",
		},
		{
			Name:      "error",
			PublicKey: "some invalid key",
			Error:     errors.New("ssh: no key found"),
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			f, err := generateSSHKeyFingerprint(test.PublicKey)

			if test.Error == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err, test.Error)
			}
			assert.Equal(t, f, test.Fingerprint)
		})
	}
}

func TestSSHKeyName(t *testing.T) {
	assert.Equal(t, "short-abcdefgh", sshKeyName("short", "ABCDEFGHIJKL"))

	long := "devpod-a-very-long-workspace-name-1234"
	name := sshKeyName(long, "ABCDEFGHIJKL")
	assert.Equal(t, long[:maxSSHKeyNameIDLength]+"-abcdefgh", name)
}

func TestCloudInitStatus(t *testing.T) {
	for out, wantDone := range map[string]bool{
		"status: done\n":          true,
		"status: degraded done\n": true,
		"status: running\n":       false,
		"status: error\n":         false,
	} {
		status, err := parseCloudInitStatus([]byte(out))
		assert.NoError(t, err)
		assert.Equal(t, wantDone, isCloudInitDone(status.Status), out)
	}

	assert.True(t, isCloudInitFailed("error"))
	assert.False(t, isCloudInitFailed("running"))
}

func TestGenerateUserData(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test\n", 42, defaultMachine(t))
	assert.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "scsi-0HC_Volume_42")
	assert.Contains(t, out, `- "ssh-ed25519 AAAA test"`)
	assert.Contains(t, out, "ufw --force enable")
}

func TestGenerateUserDataSSHDHardening(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test", 1, defaultMachine(t))
	assert.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "/etc/ssh/sshd_config.d/00-devpod.conf")
	assert.Contains(t, out, "systemctl restart ssh;")
	assert.Contains(t, out, "sed -i '1i PermitRootLogin no'")
}

func TestGenerateUserDataIsValidYAML(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test", 1, defaultMachine(t))
	assert.NoError(t, err)

	var cfg struct {
		RunCmd []any `json:"runcmd"`
	}
	assert.NoError(t, yaml.Unmarshal(buf.Bytes(), &cfg))
	assert.NotEmpty(t, cfg.RunCmd)
}

// renderedUserData parses the generated cloud-config into the parts the tests check.
type renderedUserData struct {
	Mounts [][]string
	Swap   struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	RunCmd     []string          // each command as a single shell line
	WriteFiles map[string]string // path -> content
}

// defaultMachine returns the machine options as DevPod passes them by default.
func defaultMachine(t *testing.T) *options.Machine {
	t.Helper()
	for _, env := range machineEnvs {
		t.Setenv(env, "")
	}
	m, err := options.MachineFromEnv()
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return &m
}

var machineEnvs = []string{
	options.EnvPersistDocker, options.EnvContainerdImageStore, options.EnvBuildCacheSize,
	options.EnvPruneImagesAfter, options.EnvPruneAnonymousVolumes, options.EnvSwapSize,
	options.EnvContainersMemoryHigh, options.EnvContainersMemoryMax, options.EnvContainersSwapMax,
}

func renderUserData(t *testing.T, machine *options.Machine) renderedUserData {
	t.Helper()

	buf, err := generateUserData("ssh-ed25519 AAAA test\n", 1234, machine)
	if !assert.NoError(t, err) {
		t.FailNow()
	}

	var raw struct {
		Mounts [][]string `json:"mounts"`
		Swap   struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		} `json:"swap"`
		RunCmd     []any `json:"runcmd"`
		WriteFiles []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"write_files"`
	}
	if !assert.NoError(t, yaml.Unmarshal(buf.Bytes(), &raw)) {
		t.FailNow()
	}

	out := renderedUserData{Mounts: raw.Mounts, Swap: raw.Swap, WriteFiles: map[string]string{}}
	for _, cmd := range raw.RunCmd {
		switch c := cmd.(type) {
		case string:
			out.RunCmd = append(out.RunCmd, c)
		case []any:
			parts := make([]string, 0, len(c))
			for _, p := range c {
				parts = append(parts, fmt.Sprint(p))
			}
			out.RunCmd = append(out.RunCmd, strings.Join(parts, " "))
		default:
			t.Fatalf("unexpected runcmd entry %#v", cmd)
		}
	}
	for _, f := range raw.WriteFiles {
		out.WriteFiles[f.Path] = f.Content
	}

	return out
}

// runCmdIndex returns the index of the first runcmd line containing substr, or -1.
func runCmdIndex(cmds []string, substr string) int {
	for i, c := range cmds {
		if strings.Contains(c, substr) {
			return i
		}
	}
	return -1
}

func TestGenerateUserDataOOMProtection(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))

	assert.Equal(t, "/swapfile", cfg.Swap.Filename)
	assert.Equal(t, int64(8<<30), cfg.Swap.Size)

	assert.Contains(t, cfg.WriteFiles["/etc/systemd/system/ssh.service.d/10-devpod-oom.conf"], "OOMScoreAdjust=-900")
	assert.Contains(t, cfg.WriteFiles["/etc/systemd/system/sshd.service.d/10-devpod-oom.conf"], "OOMScoreAdjust=-900")
	slice := cfg.WriteFiles["/etc/systemd/system/devpod.slice"]
	assert.Contains(t, slice, "MemoryMax=90%")
	// Measured: a throttle zone (MemoryHigh) below MemoryMax let a runaway container hang for minutes
	assert.Contains(t, slice, "MemoryHigh=infinity\n")
	assert.Contains(t, slice, "MemorySwapMax=4G\n")
}

func TestGenerateUserDataDockerOnVolume(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))

	var daemon struct {
		DataRoot     string          `json:"data-root"`
		CgroupParent string          `json:"cgroup-parent"`
		Features     map[string]bool `json:"features"`
		Builder      struct {
			GC struct {
				Enabled            bool   `json:"enabled"`
				DefaultKeepStorage string `json:"defaultKeepStorage"`
			} `json:"gc"`
		} `json:"builder"`
		LogDriver   string `json:"log-driver"`
		LiveRestore bool   `json:"live-restore"`
	}
	assert.NoError(t, json.Unmarshal([]byte(cfg.WriteFiles["/etc/docker/daemon.json"]), &daemon))
	assert.Equal(t, "/home/devpod/.devpod/docker", daemon.DataRoot)
	assert.Equal(t, "devpod.slice", daemon.CgroupParent)
	assert.True(t, daemon.Features["containerd-snapshotter"], "containerd image store must be enabled explicitly")
	assert.True(t, daemon.Builder.GC.Enabled)
	assert.Equal(t, "5GB", daemon.Builder.GC.DefaultKeepStorage)
	assert.Equal(t, "local", daemon.LogDriver)
	assert.True(t, daemon.LiveRestore)

	// The packaged containerd config is left alone: its format differs between
	// containerd 1.x and 2.x (2.x rejects the 1.x-style `disabled_plugins = ["cri"]`)
	_, written := cfg.WriteFiles["/etc/containerd/config.toml"]
	assert.False(t, written, "containerd's config.toml must not be overwritten")
	containerd := cfg.WriteFiles["/etc/systemd/system/containerd.service.d/10-devpod-volume.conf"]
	assert.Contains(t, containerd, "ExecStart=\nExecStart=/usr/bin/env containerd --root /home/devpod/.devpod/containerd\n")

	for _, unit := range []string{"docker", "containerd"} {
		assert.Contains(t, cfg.WriteFiles["/etc/systemd/system/"+unit+".service.d/10-devpod-volume.conf"],
			"RequiresMountsFor=/home/devpod", unit)
	}
}

func TestGenerateUserDataVolumeMount(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))

	if assert.Len(t, cfg.Mounts, 1) && assert.Len(t, cfg.Mounts[0], 6) {
		m := cfg.Mounts[0]
		assert.Equal(t, "/dev/disk/by-id/scsi-0HC_Volume_1234", m[0])
		assert.Equal(t, "/home/devpod", m[1])
		assert.Contains(t, m[3], "noatime")
		assert.Contains(t, m[3], "nofail")
		assert.NotContains(t, m[3], "discard", "online discard slows deletes; fstrim runs at boot instead")
	}
}

func TestGenerateUserDataDockerStartOrder(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))

	install := runCmdIndex(cfg.RunCmd, "get.docker.com")
	mounted := runCmdIndex(cfg.RunCmd, "mountpoint -q /home/devpod")
	restart := runCmdIndex(cfg.RunCmd, "systemctl restart containerd && systemctl restart docker")
	for name, i := range map[string]int{"install": install, "mountpoint": mounted, "restart": restart} {
		assert.NotEqual(t, -1, i, name)
	}
	// Docker is not installed or started without the volume
	assert.Less(t, mounted, install)
	assert.Less(t, install, restart)

	// runcmd doesn't stop on errors, so the critical steps must fail explicitly
	assert.True(t, strings.HasSuffix(cfg.RunCmd[mounted], "|| exit 1"))
	assert.True(t, strings.HasSuffix(cfg.RunCmd[restart], "|| exit 1"))
	// The restart must pick up the drop-ins and the fstab entry
	assert.Contains(t, cfg.RunCmd[restart-1], "systemctl daemon-reload")
}

func TestGenerateUserDataBootCleanup(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))
	restart := runCmdIndex(cfg.RunCmd, "systemctl restart containerd && systemctl restart docker")

	// Only these prunes are safe with several workspaces on one machine: they
	// never touch named volumes or (stopped) containers of another workspace.
	allowed := []string{
		"docker image prune --all --force --filter until=168h || true",
		// Only from Docker 23 on: older versions also prune unused named volumes
		"docker info 2>/dev/null | awk -F'[ .]+' '/Server Version/ {exit !($4 >= 23)}' && docker volume prune --force || true",
	}
	var prunes []string
	for i, c := range cfg.RunCmd {
		if !strings.Contains(c, "prune") {
			continue
		}
		prunes = append(prunes, c)
		assert.Greater(t, i, restart, "prune must run after Docker is restarted on the volume: %s", c)
	}
	assert.ElementsMatch(t, allowed, prunes)

	fstrim := runCmdIndex(cfg.RunCmd, "fstrim /home/devpod")
	if assert.NotEqual(t, -1, fstrim) {
		assert.True(t, strings.HasSuffix(cfg.RunCmd[fstrim], "|| true"))
		assert.Contains(t, cfg.RunCmd[fstrim], "systemd-run", "fstrim must not block provisioning")
	}
}

func TestGenerateUserDataMachineOptions(t *testing.T) {
	m := defaultMachine(t)
	m.PersistDocker = false
	m.ContainerdImageStore = false
	m.BuildCacheSize = "20GB"
	m.PruneImagesAfter = ""
	m.PruneAnonymousVolumes = false
	m.SwapSizeBytes = 0
	m.ContainersMemoryHigh = "6G"
	m.ContainersMemoryMax = "infinity"
	m.ContainersSwapMax = "2G"
	cfg := renderUserData(t, m)

	var daemon struct {
		DataRoot string          `json:"data-root"`
		Features map[string]bool `json:"features"`
		Builder  struct {
			GC struct {
				DefaultKeepStorage string `json:"defaultKeepStorage"`
			} `json:"gc"`
		} `json:"builder"`
	}
	assert.NoError(t, json.Unmarshal([]byte(cfg.WriteFiles["/etc/docker/daemon.json"]), &daemon))
	assert.Empty(t, daemon.DataRoot, "Docker stays on the local disk without PERSIST_DOCKER")
	assert.False(t, daemon.Features["containerd-snapshotter"])
	assert.Equal(t, "20GB", daemon.Builder.GC.DefaultKeepStorage)

	for _, unit := range []string{"docker", "containerd"} {
		_, ok := cfg.WriteFiles["/etc/systemd/system/"+unit+".service.d/10-devpod-volume.conf"]
		assert.False(t, ok, unit)
	}
	assert.Equal(t, -1, runCmdIndex(cfg.RunCmd, "prune"))
	assert.Empty(t, cfg.Swap.Filename)

	slice := cfg.WriteFiles["/etc/systemd/system/devpod.slice"]
	assert.Contains(t, slice, "MemoryHigh=6G\n")
	assert.Contains(t, slice, "MemoryMax=infinity\n")
	assert.Contains(t, slice, "MemorySwapMax=2G\n")

	// The home volume must still be mounted before Docker is restarted
	assert.NotEqual(t, -1, runCmdIndex(cfg.RunCmd, "mountpoint -q /home/devpod || exit 1"))
}

func TestGenerateUserDataPruneAge(t *testing.T) {
	m := defaultMachine(t)
	m.PruneImagesAfter = "72h"
	m.PruneAnonymousVolumes = false
	cfg := renderUserData(t, m)

	var prunes []string
	for _, c := range cfg.RunCmd {
		if strings.Contains(c, "prune") {
			prunes = append(prunes, c)
		}
	}
	assert.Equal(t, []string{"docker image prune --all --force --filter until=72h || true"}, prunes)
}

func TestGenerateUserDataNoPackageRefresh(t *testing.T) {
	buf, err := generateUserData("ssh-ed25519 AAAA test", 1, defaultMachine(t))
	assert.NoError(t, err)

	var cfg map[string]any
	assert.NoError(t, yaml.Unmarshal(buf.Bytes(), &cfg))
	// A "packages:" list makes cloud-init refresh the package lists on every start
	assert.NotContains(t, cfg, "packages")
	assert.Equal(t, false, cfg["package_update"])

	cmds := renderUserData(t, defaultMachine(t)).RunCmd
	install := runCmdIndex(cmds, "apt-get install -y -qq curl ufw")
	if assert.NotEqual(t, -1, install) {
		assert.Less(t, install, runCmdIndex(cmds, "ufw allow ssh"))
		assert.Less(t, install, runCmdIndex(cmds, "get.docker.com"))
	}
}
