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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/pkg/options"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeResizeAPI answers volume resize requests in-process (no local port, so it
// works in restricted sandboxes) and records the requested size.
type fakeResizeAPI struct {
	fail    bool
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
			rec.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(rec).Encode(map[string]any{"error": map[string]any{"code": "limit_reached", "message": "limit reached"}})
			return rec.Result(), nil
		}
		var body struct {
			Size int32 `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.newSize.Store(body.Size)
		_ = json.NewEncoder(rec).Encode(map[string]any{"action": map[string]any{"id": 1, "status": "success", "progress": 100}})
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
	api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, 50)
	assert.Equal(t, int32(1), api.calls.Load())
	assert.Equal(t, int32(50), api.newSize.Load())
}

func TestGrowVolumeNeverShrinks(t *testing.T) {
	for _, size := range []int{30, 20, 10} {
		api := &fakeResizeAPI{}
		api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, size)
		assert.Zero(t, api.calls.Load(), "requested %dGB for a 30GB volume", size)
	}
}

func TestGrowVolumeFailureIsNotFatal(t *testing.T) {
	// The machine still starts with the volume it has, so there is nothing to return or panic about
	api := &fakeResizeAPI{fail: true}
	api.hetzner().growVolume(context.Background(), &hcloud.Volume{ID: 7, Name: machineName, Size: 30}, 50)
	assert.Equal(t, int32(1), api.calls.Load())
}

func TestGenerateUserDataGrowsFilesystem(t *testing.T) {
	cfg := renderUserData(t, defaultMachine(t))

	assert.Equal(t, "auto", cfg.Mounts[0][2], "the filesystem depends on how the volume was created")

	grow := runCmdIndex(cfg.RunCmd, "xfs_growfs /home/devpod")
	if !assert.NotEqual(t, -1, grow) {
		return
	}
	line := cfg.RunCmd[grow]
	assert.Contains(t, line, "resize2fs /dev/disk/by-id/scsi-0HC_Volume_1234")
	assert.Contains(t, line, "tune2fs -m 1 /dev/disk/by-id/scsi-0HC_Volume_1234")
	assert.True(t, strings.HasSuffix(line, "|| true"), "growing must never fail provisioning")

	// After the volume is mounted, before Docker starts using it
	assert.Less(t, runCmdIndex(cfg.RunCmd, "mountpoint -q /home/devpod"), grow)
	assert.Less(t, grow, runCmdIndex(cfg.RunCmd, "systemctl restart containerd"))
}

func TestMachineVolumeFormatDefault(t *testing.T) {
	assert.Equal(t, options.DefaultVolumeFormat, defaultMachine(t).VolumeFormat)
	assert.Equal(t, "xfs", options.DefaultVolumeFormat)
}
