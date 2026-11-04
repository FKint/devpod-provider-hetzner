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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Names and JSON keys shared by the fakes in the tests
const (
	machineName = "machine"
	otherName   = "other"

	keyAction   = "action"
	keyActions  = "actions"
	keyCode     = "code"
	keyCommand  = "command"
	keyError    = "error"
	keyID       = "id"
	keyLabels   = "labels"
	keyMessage  = "message"
	keyName     = "name"
	keyProgress = "progress"
	keyServer   = "server"
	keyServers  = "servers"
	keyStatus   = "status"

	statusError   = "error"
	statusRunning = "running"
	statusSuccess = "success"

	codeUnknownError = "unknown_error"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newTestClient returns a Hetzner API client that talks to handler in-process rather
// than on a local port, so the tests work in restricted sandboxes. Polling and
// retries are made fast.
func newTestClient(t *testing.T, handler http.Handler) *hcloud.Client {
	t.Helper()

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, r)

		return rec.Result(), nil
	})

	return hcloud.NewClient(
		hcloud.WithEndpoint("http://hetzner.test"),
		hcloud.WithHTTPClient(&http.Client{Transport: transport}),
		hcloud.WithToken("test"),
		hcloud.WithPollOpts(hcloud.PollOpts{BackoffFunc: hcloud.ConstantBackoff(time.Millisecond)}),
		hcloud.WithRetryOpts(hcloud.RetryOpts{BackoffFunc: hcloud.ConstantBackoff(time.Millisecond), MaxRetries: 0}),
	)
}

// writeAPIError answers like the Hetzner API does for a failed request.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{keyError: map[string]any{keyCode: code, keyMessage: message}})
}
