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
	"io"

	"github.com/loft-sh/devpod/pkg/ssh"
	"github.com/loft-sh/log"
)

// ErrServerNotFound is returned when the machine has no server.
var ErrServerNotFound = errors.New("vm not found")

// RunCommand runs command on the machine's server over SSH, with the given streams.
func (h *Hetzner) RunCommand(
	ctx context.Context,
	name string,
	privateKey []byte,
	command string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	server, err := h.GetByName(ctx, name)
	if err != nil {
		return err
	} else if server == nil {
		return ErrServerNotFound
	}

	sshClient, err := DialSSH(ctx, server.PublicNet.IPv4.IP.String(), privateKey)
	if err != nil {
		return fmt.Errorf("create ssh client: %w", err)
	}
	defer func() {
		if err := sshClient.Close(); err != nil {
			log.Default.Debugf("Error closing SSH connection: %v", err)
		}
	}()

	return ssh.Run(ctx, sshClient, command, stdin, stdout, stderr, nil)
}
