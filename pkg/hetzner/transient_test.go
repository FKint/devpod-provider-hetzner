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
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
)

func TestIsTransient(t *testing.T) {
	apiErr := func(code hcloud.ErrorCode) error { return hcloud.Error{Code: code} }

	assert.True(t, isTransient(&net.DNSError{IsTimeout: true}), "network errors are retried")
	assert.True(t, isTransient(fmt.Errorf("wrapped: %w", &net.DNSError{})))
	assert.True(t, isTransient(apiErr(hcloud.ErrorCodeRateLimitExceeded)))
	assert.True(t, isTransient(apiErr(hcloud.ErrorCodeTimeout)))
	assert.True(t, isTransient(apiErr(hcloud.ErrorCodeServerError)))
	assert.True(t, isTransient(apiErr(hcloud.ErrorCodeConflict)), "a locked resource is released by the action holding it")
	assert.False(t, isTransient(apiErr(hcloud.ErrorCodeUnauthorized)))
	assert.False(t, isTransient(apiErr(hcloud.ErrorCodeUniquenessError)), "a duplicate name stays a duplicate")
	assert.False(t, isTransient(errors.New("boom")))
	assert.False(t, isTransient(nil))
}

func TestParseCloudInitStatusRejectsGarbage(t *testing.T) {
	_, err := parseCloudInitStatus([]byte("status: [unterminated"))
	assert.Error(t, err)
}
