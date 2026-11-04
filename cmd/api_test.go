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

package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// useFakeAPI answers the requests of the Hetzner client with handler, in-process. The
// commands build their own client on http.DefaultTransport, so replacing that is
// enough: no request leaves the test. Because it is global state, the tests using
// it must not run in parallel. Only non-retryable statuses (such as 401) should be
// returned for errors: the client retries 5xx with a real backoff.
func useFakeAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		handler(rec, r)

		return rec.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
}

const (
	pathServers = "/servers"
	pathVolumes = "/volumes"

	emptyServers  = `{"servers":[]}`
	emptyVolumes  = `{"volumes":[]}`
	runningServer = `{"servers":[{"id":5,"name":"test-machine","status":"running"}]}`
	stoppedVolume = `{"volumes":[{"id":7,"name":"test-machine"}]}`
)

// serve answers the requests whose path has an entry in routes, and 401 for the rest.
func serve(routes map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The real endpoint is https://api.hetzner.cloud/v1
		body, ok := routes[strings.TrimPrefix(r.URL.Path, "/v1")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			//nolint:misspell // the code is the API's spelling
			body = `{"error":{"code":"unauthorized","message":"denied"}}`
		}
		_, _ = io.WriteString(w, body)
	}
}

// captureStdout returns what f writes to os.Stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	f()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	return string(out)
}

func TestInitAgainstTheAPI(t *testing.T) {
	setProviderEnv(t, nil)

	useFakeAPI(t, serve(map[string]string{pathServers: emptyServers}))
	assert.NoError(t, initCmd.RunE(initCmd, nil))

	useFakeAPI(t, serve(nil))
	assert.ErrorContains(t, initCmd.RunE(initCmd, nil), "denied")
}

func TestStatusAgainstTheAPI(t *testing.T) {
	tests := []struct {
		name   string
		routes map[string]string
		want   string
	}{
		{"running", map[string]string{pathServers: runningServer}, "Running"},
		{"stopped", map[string]string{pathServers: emptyServers, pathVolumes: stoppedVolume}, "Stopped"},
		{"not found", map[string]string{pathServers: emptyServers, pathVolumes: emptyVolumes}, "NotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setProviderEnv(t, nil)
			useFakeAPI(t, serve(tt.routes))

			out := captureStdout(t, func() { assert.NoError(t, statusCmd.RunE(statusCmd, nil)) })
			assert.Equal(t, tt.want, out)
		})
	}

	t.Run("API error", func(t *testing.T) {
		setProviderEnv(t, nil)
		useFakeAPI(t, serve(nil))
		assert.Error(t, statusCmd.RunE(statusCmd, nil))
	})
}

func TestStopAgainstTheAPI(t *testing.T) {
	setProviderEnv(t, nil)

	// No server is left and the volume is free, so the machine counts as stopped at once
	useFakeAPI(t, serve(map[string]string{pathServers: emptyServers, pathVolumes: stoppedVolume}))
	assert.NoError(t, stopCmd.RunE(stopCmd, nil))

	useFakeAPI(t, serve(nil))
	assert.Error(t, stopCmd.RunE(stopCmd, nil))
}

func TestCreateAndStartReportAPIErrors(t *testing.T) {
	for name, routes := range map[string]map[string]string{
		"API error":      nil,
		"unknown region": {"/locations": `{"locations":[]}`},
	} {
		t.Run(name, func(t *testing.T) {
			setProviderEnv(t, nil)
			useFakeAPI(t, serve(routes))

			assert.Error(t, createCmd.RunE(createCmd, nil))
			assert.Error(t, startCmd.RunE(startCmd, nil))
		})
	}
}

func TestCommandReportsAMissingServer(t *testing.T) {
	setProviderEnv(t, nil)
	t.Setenv("COMMAND", "ls")

	useFakeAPI(t, serve(map[string]string{pathServers: emptyServers}))
	assert.ErrorContains(t, commandCmd.RunE(commandCmd, nil), "vm not found")

	useFakeAPI(t, serve(nil))
	assert.Error(t, commandCmd.RunE(commandCmd, nil))
}
