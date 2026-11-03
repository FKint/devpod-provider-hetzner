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

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/loft-sh/log"
)

// growVolume enlarges an existing volume to size GB if it is smaller, so a bigger
// DISK_SIZE applies to a machine that already has its volume. Hetzner volumes
// can't shrink, so a larger volume is left alone. The filesystem is grown on the
// server (see cloud-config.yaml). A failure is not fatal: the machine still starts
// with the volume it has.
func (h *Hetzner) growVolume(ctx context.Context, volume *hcloud.Volume, size int) {
	if size <= volume.Size {
		return
	}

	log.Default.Infof("Growing volume %s from %dGB to %dGB", volume.Name, volume.Size, size)

	action, _, err := h.client.Volume.Resize(ctx, volume, size)
	if err == nil {
		err = h.waitForActions(ctx, action)
	}
	if err != nil {
		log.Default.Warnf("Unable to grow volume %s to %dGB, continuing with %dGB: %v", volume.Name, size, volume.Size, err)
	}
}
