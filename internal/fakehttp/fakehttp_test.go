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

package fakehttp_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/FKint/devpod-provider-hetzner/internal/fakehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// get fetches a path and returns the status, content type and body.
func get(t *testing.T, client *http.Client) (status int, contentType, body string) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://hetzner.test/ping", http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, resp.Header.Get("Content-Type"), string(data)
}

func TestTransport(t *testing.T) {
	teapot := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "brew")
	})
	status, contentType, body := get(t, &http.Client{Transport: fakehttp.Transport(teapot)})

	assert.Equal(t, http.StatusTeapot, status)
	assert.Equal(t, "application/json", contentType)
	assert.Equal(t, "brew", body)
}

func TestReplaceDefaultTransportRestoresTheOriginal(t *testing.T) {
	original := http.DefaultTransport

	t.Run("replaced", func(t *testing.T) {
		fakehttp.ReplaceDefaultTransport(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "fake")
		}))

		_, _, body := get(t, &http.Client{})
		assert.Equal(t, "fake", body)
	})

	assert.Same(t, original, http.DefaultTransport)
}
