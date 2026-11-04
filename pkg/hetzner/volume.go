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
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/log"
)

// attachedLiveServer returns the server an attached volume is held by, or nil if
// that server is gone or being deleted. Hetzner reports a
// server delete as done before it has released its volumes, so for a short while
// a volume can still point at a server that no longer exists.
func (h *Hetzner) attachedLiveServer(ctx context.Context, volume *hcloud.Volume) (*hcloud.Server, error) {
	server, _, err := h.client.Server.GetByID(ctx, volume.Server.ID)
	if err != nil {
		return nil, err
	}
	if server == nil || server.Status == hcloud.ServerStatusDeleting {
		return nil, nil
	}

	return server, nil
}

// detachCheckRetries is how many API errors in a row end the wait.
const detachCheckRetries = 5

// waitForVolumeDetached returns the named volume once no server holds it any
// more (nil if there is no such volume). Creating a server with a volume that is
// still attached fails with "volume already attached", which happens right after
// a stop because the server delete completes before the detach does.
//
// A volume attached to a server that is still alive is not waited for: it
// belongs to something else, and waiting would not help. A few transient API
// errors are retried.
func (h *Hetzner) waitForVolumeDetached(ctx context.Context, name string) (*hcloud.Volume, error) {
	ctx, cancel := context.WithTimeout(ctx, volumeDetachTimeout)
	defer cancel()

	poll := h.detachPoll
	if poll <= 0 {
		poll = statusPoll
	}

	logged, failures := false, 0
	for {
		volume, live, err := h.volumeAndHolder(ctx, name)
		switch {
		case err != nil && ctx.Err() != nil:
			return nil, fmt.Errorf("waiting for volume %s to detach: %w", name, ctx.Err())
		case err != nil && (!isTransient(err) || failures >= detachCheckRetries):
			return nil, err
		case err != nil:
			failures++
			log.Default.Warnf("Error checking volume %s, retrying: %v", name, err)
		case volume == nil || volume.Server == nil:
			return volume, nil
		case live != nil:
			return nil, fmt.Errorf("%w: volume %s is attached to server %s", ErrVolumeAttached, name, live.Name)
		default:
			failures = 0
			if !logged {
				log.Default.Infof("Waiting for volume %s to detach from the previous server", name)
				logged = true
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for volume %s to detach: %w", name, ctx.Err())
		case <-time.After(poll):
		}
	}
}

// isTransient reports whether an API error is worth retrying: network trouble,
// rate limiting or a server-side hiccup. Anything else (bad token, duplicate
// volume name, ...) will not fix itself.
func isTransient(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	return hcloud.IsError(err,
		hcloud.ErrorCodeRateLimitExceeded,
		hcloud.ErrorCodeTimeout,
		hcloud.ErrorCodeServerError,
		hcloud.ErrorCodeConflict,
	)
}

// volumeAndHolder looks up the volume and, if it is attached, the live server holding it.
func (h *Hetzner) volumeAndHolder(ctx context.Context, name string) (*hcloud.Volume, *hcloud.Server, error) {
	volume, err := h.volumeByName(ctx, name)
	if err != nil || volume == nil || volume.Server == nil {
		return volume, nil, err
	}

	live, err := h.attachedLiveServer(ctx, volume)

	return volume, live, err
}
