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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeResizeAPI answers volume resize requests in-process (no local port, so it
// works in restricted sandboxes) and records the requested size.
type fakeResizeAPI struct {
	fail    bool
	pending bool // the action is accepted but never completes (and can't be polled)
	calls   atomic.Int32
	newSize atomic.Int32
}

func (f *fakeResizeAPI) hetzner() *Hetzner {
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/volumes/7/actions/resize") {
			rec.WriteHeader(http.StatusNotFound)
			return rec.Result(), nil
		}
		f.calls.Add(1)
		if f.fail {
			writeAPIError(rec, http.StatusForbidden, "limit_reached", "limit reached")
			return rec.Result(), nil
		}
		var body struct {
			Size int32 `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.newSize.Store(body.Size)
		status := "success"
		if f.pending {
			status = "running"
		}
		_ = json.NewEncoder(rec).Encode(map[string]any{"action": map[string]any{"id": 1, "status": status}})
		return rec.Result(), nil
	})

	return &Hetzner{client: hcloud.NewClient(
		hcloud.WithEndpoint("http://hetzner.test"),
		hcloud.WithHTTPClient(&http.Client{Transport: transport}),
		hcloud.WithToken("test"),
		hcloud.WithRetryOpts(hcloud.RetryOpts{MaxRetries: 0}),
	)}
}

const machineName = "machine"

func TestGrowVolume(t *testing.T) {
	api := &fakeResizeAPI{}
	assert.NoError(t, api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, 50))
	assert.Equal(t, int32(1), api.calls.Load())
	assert.Equal(t, int32(50), api.newSize.Load())
}

func TestGrowVolumeNeverShrinks(t *testing.T) {
	for _, size := range []int{30, 20, 10} {
		api := &fakeResizeAPI{}
		assert.NoError(t, api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, size))
		assert.Zero(t, api.calls.Load(), "requested %dGB for a 30GB volume", size)
	}
}

func TestGrowVolumeRefusedIsNotFatal(t *testing.T) {
	// The machine still starts with the volume it has
	api := &fakeResizeAPI{fail: true}
	assert.NoError(t, api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, 50))
	assert.Equal(t, int32(1), api.calls.Load())
}

func TestGrowVolumeUnconfirmedIsAnError(t *testing.T) {
	// The resize was accepted but isn't confirmed: the volume may be locked, so don't attach it
	api := &fakeResizeAPI{pending: true}
	err := api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, 50)
	assert.ErrorContains(t, err, "growing volume")
}

func TestGenerateUserDataGrowsFilesystem(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))

	assert.Equal(t, "auto", cfg.Mounts[0][2], "the filesystem depends on how the volume was created")

	grow := runCmdIndex(cfg.RunCmd, "xfs_growfs /home/devpod")
	if !assert.NotEqual(t, -1, grow) {
		return
	}
	line := cfg.RunCmd[grow]
	assert.Contains(t, line, "resize2fs $d")
	assert.Contains(t, line, "tune2fs -m 1 $d")
	assert.Contains(t, line, "|| echo", "growing must never fail provisioning, but a failure must show in the cloud-init log")

	// After the volume is mounted, before Docker starts using it
	assert.Less(t, runCmdIndex(cfg.RunCmd, "mountpoint -q /home/devpod"), grow)
	assert.Less(t, grow, runCmdIndex(cfg.RunCmd, "systemctl restart containerd"))
}

// writeStubs puts fake filesystem tools on a PATH directory; each one appends
// its name and arguments to the returned log file.
func writeStubs(t *testing.T, fstype string) (path, logFile string) {
	t.Helper()
	dir := t.TempDir()
	logFile = filepath.Join(dir, "calls.log")
	script := func(name, body string) {
		assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700)) //nolint:gosec // test stub
	}
	script("findmnt", `case "$*" in *FSTYPE*) echo `+fstype+`;; *SOURCE*) echo /dev/sdb;; esac`)
	for _, tool := range []string{"xfs_growfs", "resize2fs", "tune2fs"} {
		script(tool, `echo "`+tool+` $*" >> `+logFile)
	}

	return dir + ":/usr/bin:/bin", logFile
}

// The grow step is a shell one-liner that only string checks would not catch breaking.
func TestGrowFilesystemCommand(t *testing.T) {
	cmds := renderUserData(t, defaultMachine(t)).RunCmd
	line := cmds[runCmdIndex(cmds, "xfs_growfs")]

	for fstype, want := range map[string]string{
		"xfs":   "xfs_growfs /home/devpod\n",
		"ext4":  "resize2fs /dev/sdb\ntune2fs -m 1 /dev/sdb\n",
		"btrfs": "",
	} {
		path, logFile := writeStubs(t, fstype)
		cmd := exec.CommandContext(context.Background(), "sh", "-c", line)
		cmd.Env = []string{"PATH=" + path}
		out, err := cmd.CombinedOutput()
		assert.NoError(t, err, "%s: %s", fstype, out)

		got, _ := os.ReadFile(logFile)
		assert.Equal(t, want, string(got), fstype)
	}
}

func TestGrowFilesystemCommandReportsFailure(t *testing.T) {
	cmds := renderUserData(t, defaultMachine(t)).RunCmd
	line := cmds[runCmdIndex(cmds, "xfs_growfs")]

	// Tools missing entirely: findmnt says xfs, but xfs_growfs doesn't exist
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "findmnt"), []byte("#!/bin/sh\necho xfs\n"), 0o700)) //nolint:gosec // test stub
	cmd := exec.CommandContext(context.Background(), "sh", "-c", line)
	cmd.Env = []string{"PATH=" + dir}
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, "provisioning must continue")
	assert.Contains(t, string(out), "could not grow the filesystem")
}
