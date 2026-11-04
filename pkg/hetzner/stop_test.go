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
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

const (
	callShutdown = "shutdown"
	callDelete   = "delete"
)

// fakeStopAPI serves one running server and records the calls that matter.
type fakeStopAPI struct {
	mu    sync.Mutex
	calls []string
	// shutdownFails makes the shutdown request fail
	shutdownFails bool
	// offAfter is how many status lookups report "running" before the server is off;
	// negative means it never powers off.
	offAfter int
	lookups  int
}

func (f *fakeStopAPI) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeStopAPI) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeStopAPI) handler() http.Handler {
	server := func(status string) map[string]any {
		return map[string]any{keyID: 5, keyName: "machine", keyStatus: status}
	}
	action := map[string]any{"action": map[string]any{keyID: 1, keyStatus: "success", "progress": 100}}
	mux := http.NewServeMux()
	mux.HandleFunc("/servers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []any{server("running")}})
	})
	mux.HandleFunc("/servers/5/actions/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		f.record(callShutdown)
		if f.shutdownFails {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{keyError: map[string]any{"code": "locked", "message": "server locked"}})
			return
		}
		_ = json.NewEncoder(w).Encode(action)
	})
	mux.HandleFunc("/actions", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"actions": []any{action["action"]}})
	})
	mux.HandleFunc("/servers/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			f.record(callDelete)
			_ = json.NewEncoder(w).Encode(action)
			return
		}
		f.mu.Lock()
		f.lookups++
		off := f.offAfter >= 0 && f.lookups > f.offAfter
		f.mu.Unlock()
		status := "running"
		if off {
			status = "off"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{keyServer: server(status)})
	})

	return mux
}

func (f *fakeStopAPI) hetzner(t *testing.T) *Hetzner {
	t.Helper()
	handler := f.handler()
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
			hcloud.WithPollOpts(hcloud.PollOpts{BackoffFunc: hcloud.ConstantBackoff(time.Millisecond)}),
			hcloud.WithRetryOpts(hcloud.RetryOpts{BackoffFunc: hcloud.ConstantBackoff(time.Millisecond), MaxRetries: 0}),
		),
		shutdownPoll:    time.Millisecond,
		shutdownTimeout: 200 * time.Millisecond,
		runLocal: func(_ context.Context, name string, _ ...string) error {
			f.record("local:" + name)
			return nil
		},
	}
}

func TestStopShutsDownBeforeDeleting(t *testing.T) {
	api := &fakeStopAPI{offAfter: 2}
	assert.NoError(t, api.hetzner(t).Stop(context.Background(), "machine", false))
	assert.Equal(t, []string{callShutdown, callDelete}, api.called())
	assert.Greater(t, api.lookups, 2, "must wait for the server to be off")
}

func TestStopDeletesWhenShutdownFails(t *testing.T) {
	api := &fakeStopAPI{shutdownFails: true}
	assert.NoError(t, api.hetzner(t).Stop(context.Background(), "machine", false))
	assert.Equal(t, []string{callShutdown, callDelete}, api.called())
}

func TestStopDeletesWhenServerNeverPowersOff(t *testing.T) {
	api := &fakeStopAPI{offAfter: -1}
	assert.NoError(t, api.hetzner(t).Stop(context.Background(), "machine", false))
	assert.Equal(t, []string{callShutdown, callDelete}, api.called())
}

func TestStopFromMachineNeverShutsDown(t *testing.T) {
	api := &fakeStopAPI{}
	assert.NoError(t, api.hetzner(t).Stop(context.Background(), "machine", true))
	// containers are stopped, disks synced, then the server is deleted: no ACPI shutdown
	assert.Equal(t, []string{"local:sh", "local:sync", callDelete}, api.called())
}

func TestStopFromMachineDeletesWhenLocalStepsFail(t *testing.T) {
	api := &fakeStopAPI{}
	h := api.hetzner(t)
	h.runLocal = func(context.Context, string, ...string) error { return errors.New("docker is gone") }
	assert.NoError(t, h.Stop(context.Background(), "machine", true))
	assert.Equal(t, []string{callDelete}, api.called())
}

// JSON keys shared by the fake API servers in the tests
const (
	keyError  = "error"
	keyID     = "id"
	keyName   = "name"
	keyServer = "server"
	keyStatus = "status"
)
