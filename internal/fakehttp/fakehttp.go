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

// Package fakehttp answers HTTP requests in-process, so tests of code that calls the
// Hetzner API need no local port and work in restricted sandboxes.
package fakehttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Transport serves every request it gets with handler, responding with JSON unless the
// handler sets another Content-Type. Only non-retryable statuses (such as 401) should
// be returned for errors: the hcloud client retries 5xx with a real backoff.
func Transport(handler http.Handler) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, r)

		return rec.Result(), nil
	})
}

// ReplaceDefaultTransport makes handler answer everything sent on http.DefaultTransport,
// for code that builds its own client, and restores the original when the test ends.
// It is global state: the tests using it must not run in parallel.
func ReplaceDefaultTransport(t testing.TB, handler http.Handler) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = Transport(handler)
	t.Cleanup(func() { http.DefaultTransport = original })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
