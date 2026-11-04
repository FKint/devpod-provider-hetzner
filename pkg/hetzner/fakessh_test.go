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
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	cryptoSsh "golang.org/x/crypto/ssh"
)

// memPipe is one direction of an in-memory connection. Unlike net.Pipe it buffers, so
// both ends can write first, as the two sides of an SSH handshake do.
type memPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	data   []byte
	closed bool
}

func newMemPipe() *memPipe {
	p := &memPipe{}
	p.cond = sync.NewCond(&p.mu)

	return p
}

func (p *memPipe) write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	p.data = append(p.data, b...)
	p.cond.Broadcast()

	return len(b), nil
}

func (p *memPipe) read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.data) == 0 && !p.closed {
		p.cond.Wait()
	}
	if len(p.data) == 0 {
		return 0, io.EOF
	}
	n := copy(b, p.data)
	p.data = p.data[n:]

	return n, nil
}

func (p *memPipe) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.cond.Broadcast()
}

// memConn is one end of an in-memory connection. Closing it unblocks both ends.
type memConn struct {
	in, out *memPipe
}

func newMemConnPair() (client, server *memConn) {
	a, b := newMemPipe(), newMemPipe()

	return &memConn{in: a, out: b}, &memConn{in: b, out: a}
}

func (c *memConn) Read(b []byte) (int, error)         { return c.in.read(b) }
func (c *memConn) Write(b []byte) (int, error)        { return c.out.write(b) }
func (c *memConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *memConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *memConn) SetDeadline(_ time.Time) error      { return nil }
func (c *memConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *memConn) SetWriteDeadline(_ time.Time) error { return nil }
func (c *memConn) Close() error {
	c.in.close()
	c.out.close()

	return nil
}

// newSSHKeyPair returns a PEM private key (as the provider stores it) and the
// matching authorized_keys line.
func newSSHKeyPair(t *testing.T) (privatePEM []byte, authorizedKey string) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := cryptoSsh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	sshPub, err := cryptoSsh.NewPublicKey(pub)
	require.NoError(t, err)

	return pem.EncodeToMemory(block), string(cryptoSsh.MarshalAuthorizedKey(sshPub))
}

// fakeSSHD is an in-process SSH server answering the commands the provider runs on a
// new machine. It is reached through dialTCP, so no port is opened.
type fakeSSHD struct {
	hostKey cryptoSsh.Signer
	// authorized is the only key accepted; nil refuses every key
	authorized cryptoSsh.PublicKey
	// unreachable makes the TCP connection fail
	unreachable bool
	// refuseFirst makes that many connection attempts fail before they start succeeding
	refuseFirst int
	// hang accepts connections but never answers the handshake
	hang bool
	// dfOutput is what the disk usage command prints
	dfOutput string

	mu sync.Mutex
	// statuses are the cloud-init states reported, one per check; the last repeats
	statuses []string
	commands []string
	dials    int
	conns    []*memConn
}

// newFakeSSHD starts a fake sshd that accepts the returned private key, and installs
// it as the way to reach any server for the rest of the test.
func newFakeSSHD(t *testing.T, statuses ...string) (d *fakeSSHD, privateKey []byte) {
	t.Helper()

	privateKey, authorizedKey := newSSHKeyPair(t)
	//nolint:dogsled // only the key is wanted
	authorized, _, _, _, err := cryptoSsh.ParseAuthorizedKey([]byte(authorizedKey))
	require.NoError(t, err)

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := cryptoSsh.NewSignerFromKey(hostPriv)
	require.NoError(t, err)

	d = &fakeSSHD{hostKey: hostKey, authorized: authorized, statuses: statuses}
	previous := dialTCP
	dialTCP = d.dial
	t.Cleanup(func() {
		dialTCP = previous
		// Closing what is still open also ends the server goroutines
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, c := range d.conns {
			_ = c.Close()
		}
	})

	return d, privateKey
}

func (d *fakeSSHD) dial(ctx context.Context, target string) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.refuseFirst > 0 {
		d.refuseFirst--
		return nil, errors.New("connect: connection refused (" + target + ")")
	}
	if d.unreachable {
		return nil, errors.New("connect: connection refused (" + target + ")")
	}

	client, server := newMemConnPair()
	d.conns = append(d.conns, client, server)
	if !d.hang {
		go d.serve(server)
	}

	return client, nil
}

func (d *fakeSSHD) dialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.dials
}

// ran returns the commands run so far.
func (d *fakeSSHD) ran() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]string(nil), d.commands...)
}

func (d *fakeSSHD) serve(conn net.Conn) {
	cfg := &cryptoSsh.ServerConfig{
		PublicKeyCallback: func(_ cryptoSsh.ConnMetadata, key cryptoSsh.PublicKey) (*cryptoSsh.Permissions, error) {
			if d.authorized != nil && bytes.Equal(key.Marshal(), d.authorized.Marshal()) {
				return nil, nil
			}

			return nil, errors.New("key refused")
		},
	}
	cfg.AddHostKey(d.hostKey)

	_, chans, reqs, err := cryptoSsh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	go cryptoSsh.DiscardRequests(reqs)
	for nc := range chans {
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go d.session(ch, creqs)
	}
}

func (d *fakeSSHD) session(ch cryptoSsh.Channel, reqs <-chan *cryptoSsh.Request) {
	defer func() { _ = ch.Close() }()
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := cryptoSsh.Unmarshal(req.Payload, &payload); err != nil {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		_, _ = ch.Write([]byte(d.output(payload.Command)))
		_, _ = ch.SendRequest("exit-status", false, cryptoSsh.Marshal(struct{ Status uint32 }{0}))

		return
	}
}

func (d *fakeSSHD) output(command string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands = append(d.commands, command)

	switch {
	case strings.HasPrefix(command, "cloud-init status"):
		status := statusRunning
		if len(d.statuses) > 0 {
			status = d.statuses[0]
			if len(d.statuses) > 1 {
				d.statuses = d.statuses[1:]
			}
		}

		return "status: " + status + "\n"
	case strings.HasPrefix(command, "echo "):
		return strings.TrimPrefix(command, "echo ") + "\n"
	case strings.HasPrefix(command, "df "):
		return d.dfOutput
	}

	return ""
}
