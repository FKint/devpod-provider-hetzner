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
	"sync/atomic"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/devpod/pkg/client"
	"github.com/stretchr/testify/assert"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeAPI serves just the endpoints used to find a volume and the server it is attached to.
type fakeAPI struct {
	// attachedFor says how many volume lookups report the volume as attached to
	// server 42 before it is released; negative means never.
	attachedFor int
	// server42 is what GET /servers/42 returns: "" = not found, otherwise its status
	server42 string

	volumeCalls atomic.Int32
	serverCalls atomic.Int32
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/volumes", func(w http.ResponseWriter, _ *http.Request) {
		n := int(f.volumeCalls.Add(1))
		vol := map[string]any{"id": 7, "name": "machine", "size": 40, "server": nil}
		if f.attachedFor < 0 || n <= f.attachedFor {
			vol["server"] = 42
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"volumes": []any{vol}})
	})
	mux.HandleFunc("/servers/42", func(w http.ResponseWriter, _ *http.Request) {
		f.serverCalls.Add(1)
		if f.server42 == "" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "not_found", "message": "server not found"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"server": map[string]any{"id": 42, "name": "other", "status": f.server42}})
	})
	mux.HandleFunc("/servers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []any{}})
	})

	return mux
}

func (f *fakeAPI) hetzner(t *testing.T) *Hetzner {
	t.Helper()
	handler := f.handler(t)
	// Serve in-process rather than on a local port, so it works in restricted sandboxes
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, r)
		return rec.Result(), nil
	})

	return &Hetzner{
		client: hcloud.NewClient(
			hcloud.WithEndpoint("http://hetzner.test"),
			hcloud.WithHTTPClient(&http.Client{Transport: transport}),
			hcloud.WithToken("test"),
			hcloud.WithRetryOpts(hcloud.RetryOpts{BackoffFunc: hcloud.ConstantBackoff(time.Millisecond), MaxRetries: 0}),
		),
		detachPoll: time.Millisecond,
	}
}

func TestWaitForVolumeDetached(t *testing.T) {
	// The server delete is done but the volume still points at it for a couple of lookups
	api := &fakeAPI{attachedFor: 3}
	vol, err := api.hetzner(t).waitForVolumeDetached(context.Background(), "machine")
	assert.NoError(t, err)
	if assert.NotNil(t, vol) {
		assert.Nil(t, vol.Server)
	}
	assert.Equal(t, int32(4), api.volumeCalls.Load())
}

func TestWaitForVolumeDetachedAlreadyFree(t *testing.T) {
	api := &fakeAPI{}
	vol, err := api.hetzner(t).waitForVolumeDetached(context.Background(), "machine")
	assert.NoError(t, err)
	assert.NotNil(t, vol)
	assert.Equal(t, int32(1), api.volumeCalls.Load())
	assert.Zero(t, api.serverCalls.Load(), "no need to look at servers for a free volume")
}

func TestWaitForVolumeDetachedWhileServerIsDeleting(t *testing.T) {
	api := &fakeAPI{attachedFor: 2, server42: string(hcloud.ServerStatusDeleting)}
	vol, err := api.hetzner(t).waitForVolumeDetached(context.Background(), "machine")
	assert.NoError(t, err)
	assert.NotNil(t, vol)
}

func TestWaitForVolumeDetachedLiveServer(t *testing.T) {
	// Attached to a running server: waiting would not help, so fail right away
	api := &fakeAPI{attachedFor: -1, server42: string(hcloud.ServerStatusRunning)}
	_, err := api.hetzner(t).waitForVolumeDetached(context.Background(), "machine")
	assert.ErrorIs(t, err, ErrVolumeAttached)
	assert.ErrorContains(t, err, "other")
	assert.Equal(t, int32(1), api.volumeCalls.Load())
}

func TestWaitForVolumeDetachedTimeout(t *testing.T) {
	api := &fakeAPI{attachedFor: -1}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := api.hetzner(t).waitForVolumeDetached(ctx, "machine")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "detach")
	assert.Greater(t, api.volumeCalls.Load(), int32(1))
}

func TestStatusBusyUntilVolumeReleased(t *testing.T) {
	live, deleting := string(hcloud.ServerStatusRunning), string(hcloud.ServerStatusDeleting)
	for name, tc := range map[string]struct {
		api  *fakeAPI
		want client.Status
	}{
		"volume still held by the deleted server": {&fakeAPI{attachedFor: -1}, client.StatusBusy},
		"volume held by a server being deleted":   {&fakeAPI{attachedFor: -1, server42: deleting}, client.StatusBusy},
		"volume released":                         {&fakeAPI{}, client.StatusStopped},
		// Not this machine's server, but also not ours to wait for
		"volume held by a live server": {&fakeAPI{attachedFor: -1, server42: live}, client.StatusStopped},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := tc.api.hetzner(t).Status(context.Background(), "machine")
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
