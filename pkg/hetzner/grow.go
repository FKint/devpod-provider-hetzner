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
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/log"
)

// growVolume enlarges an existing volume to size GB if it is smaller (they never
// shrink); the server grows the filesystem at boot. If Hetzner refuses the resize
// it only warns and the machine starts with the volume it has. If a resize was
// accepted but isn't confirmed, the volume may be locked, so it is an error.
func (h *Hetzner) growVolume(ctx context.Context, volume *hcloud.Volume, size int) error {
	if size <= volume.Size {
		return nil
	}

	log.Default.Infof("Growing volume %s from %dGB to %dGB", volume.Name, volume.Size, size)

	action, _, err := h.client.Volume.Resize(ctx, volume, size)
	if err != nil {
		log.Default.Warnf("Unable to grow volume %s to %dGB, continuing with %dGB: %v", volume.Name, size, volume.Size, err)
		return nil
	}

	if err := h.waitForActions(ctx, action); err != nil {
		return fmt.Errorf("growing volume %s to %dGB: %w", volume.Name, size, err)
	}

	return nil
}
