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
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fakeRegion      = "nbg1"
	fakeMachineType = "cx23"
	fakeImage       = "docker-ce"
	fakeServerIP    = "192.0.2.10"
	// fakeServerDisk is the local disk of fakeMachineType in GB
	fakeServerDisk = 40
)

type (
	cloudObject = map[string]any
	// cloudServer, cloudVolume and cloudKey are the stored form of the API objects
	cloudServer struct {
		ID      int64
		Name    string
		Status  string
		Labels  map[string]string
		Volumes []int64
	}
	cloudVolume struct {
		ID     int64
		Name   string
		Size   int
		Server int64 // 0 when detached
		Labels map[string]string
	}
	cloudKey struct {
		ID          int64
		Name        string
		Fingerprint string
		PublicKey   string
		Labels      map[string]string
	}
)

// fakeCloud is a stateful, in-process fake of the parts of the Hetzner Cloud API the
// provider uses: servers, volumes, SSH keys, actions and the lookup endpoints. State
// changes are real (a created volume can be listed, deleting a server releases its
// volume), so tests assert on what is left behind rather than on call sequences.
type fakeCloud struct {
	mu     sync.Mutex
	nextID int64

	servers map[int64]*cloudServer
	volumes map[int64]*cloudVolume
	keys    map[int64]*cloudKey

	// requests is every request served, as "METHOD /path"
	requests []string
	// failing makes requests matching "METHOD /path" (the path without ids, e.g.
	// "POST /servers" or "DELETE /volumes/*") fail with a server error
	failing map[string]bool
	// serverStatusOnCreate is the status a created server reports (default statusRunning)
	serverStatusOnCreate string
	// unknown makes the lookup of that kind find nothing: "location", "server_type" or "image"
	unknown map[string]bool
	// lastServerBody is the body of the last server create request
	lastServerBody cloudObject
}

func newFakeCloud() *fakeCloud {
	return &fakeCloud{
		nextID:  100,
		servers: map[int64]*cloudServer{},
		volumes: map[int64]*cloudVolume{},
		keys:    map[int64]*cloudKey{},
		failing: map[string]bool{},
		unknown: map[string]bool{},
	}
}

func (f *fakeCloud) id() int64 {
	f.nextID++

	return f.nextID
}

// fail makes the matching requests fail, e.g. fail("POST /servers").
func (f *fakeCloud) fail(patterns ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range patterns {
		f.failing[p] = true
	}
}

// heal stops all injected failures.
func (f *fakeCloud) heal() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.failing)
}

// seedVolume adds an existing volume, as left behind by a stopped machine.
func (f *fakeCloud) seedVolume(name string, size int) *cloudVolume {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := &cloudVolume{ID: f.id(), Name: name, Size: size, Labels: machineLabels(name)}
	f.volumes[v.ID] = v

	return v
}

// seedServer adds an existing server with the given status.
func (f *fakeCloud) seedServer(name, status string) *cloudServer {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := &cloudServer{ID: f.id(), Name: name, Status: status, Labels: machineLabels(name)}
	f.servers[s.ID] = s

	return s
}

// seedKey adds an SSH key labelled as belonging to the machine.
func (f *fakeCloud) seedKey(machineID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := &cloudKey{ID: f.id(), Name: machineID + "-key", Labels: machineLabels(machineID)}
	f.keys[k.ID] = k
}

// left returns the names of the servers, volumes and SSH keys that exist, sorted.
func (f *fakeCloud) left() (servers, volumes, keys []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.servers {
		servers = append(servers, s.Name)
	}
	for _, v := range f.volumes {
		volumes = append(volumes, v.Name)
	}
	for _, k := range f.keys {
		keys = append(keys, k.Name)
	}
	slices.Sort(servers)
	slices.Sort(volumes)
	slices.Sort(keys)

	return servers, volumes, keys
}

// order returns the position of the first request matching "METHOD /path", or -1.
func (f *fakeCloud) order(request string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Index(f.requests, request)
}

// count returns how many requests matching "METHOD /path" were served.
func (f *fakeCloud) count(request string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return strings.Count(strings.Join(f.requests, "\n")+"\n", request+"\n")
}

func (f *fakeCloud) hetzner(t *testing.T) *Hetzner {
	t.Helper()

	return &Hetzner{client: newTestClient(t, f), poll: time.Millisecond, shutdownTimeout: 200 * time.Millisecond}
}

// pattern turns a request path into the form used by failing: ids become "*".
func pattern(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, p := range parts {
		if _, err := strconv.ParseInt(p, 10, 64); err == nil {
			parts[i] = "*"
		}
	}

	return r.Method + " /" + strings.Join(parts, "/")
}

// route is a handler of the fake; it runs with f.mu held.
type route func(f *fakeCloud, w http.ResponseWriter, r *http.Request)

// routes maps "METHOD /path" (ids as "*") to its handler.
var routes = map[string]route{
	"GET /actions": func(_ *fakeCloud, w http.ResponseWriter, _ *http.Request) {
		reply(w, cloudObject{"actions": []any{}})
	},
	"GET /locations": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		f.lookup(w, r, "location", "locations", cloudObject{keyID: 1, keyName: fakeRegion})
	},
	"GET /server_types": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		f.lookup(w, r, "server_type", "server_types", f.serverType())
	},
	"GET /images": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		f.lookup(w, r, "image", "images", cloudObject{keyID: 3, keyName: fakeImage, "type": "system", "architecture": "x86"})
	},
	"GET /ssh_keys":  (*fakeCloud).listKeys,
	"POST /ssh_keys": (*fakeCloud).createKey,
	"DELETE /ssh_keys/*": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		delete(f.keys, pathID(r))
		reply(w, cloudObject{})
	},
	"GET /servers":      (*fakeCloud).listServers,
	"POST /servers":     (*fakeCloud).createServer,
	"GET /servers/*":    (*fakeCloud).getServer,
	"DELETE /servers/*": (*fakeCloud).deleteServer,
	"POST /servers/*/actions/poweron": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		f.setServerStatus(w, r, statusRunning)
	},
	"POST /servers/*/actions/shutdown": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		f.setServerStatus(w, r, "off")
	},
	"GET /volumes":      (*fakeCloud).listVolumes,
	"POST /volumes":     (*fakeCloud).createVolume,
	"DELETE /volumes/*": (*fakeCloud).deleteVolume,
	"POST /volumes/*/actions/detach": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		if v := f.volumes[pathID(r)]; v != nil {
			v.Server = 0
		}
		reply(w, cloudObject{keyAction: fakeAction()})
	},
	"POST /volumes/*/actions/resize": func(f *fakeCloud, w http.ResponseWriter, r *http.Request) {
		var body struct {
			Size int `json:"size"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v := f.volumes[pathID(r)]; v != nil {
			v.Size = body.Size
		}
		reply(w, cloudObject{keyAction: fakeAction()})
	},
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p := pattern(r)
	f.requests = append(f.requests, p)
	if f.failing[p] {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "injected failure for "+p)
		return
	}

	handle, ok := routes[p]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "fake cloud has no "+p)
		return
	}
	handle(f, w, r)
}

func reply(w http.ResponseWriter, body cloudObject) { _ = json.NewEncoder(w).Encode(body) }

func fakeAction() cloudObject {
	return cloudObject{keyID: 1, keyStatus: statusSuccess, "progress": 100, "command": "fake"}
}

func pathID(r *http.Request) int64 {
	for _, p := range strings.Split(r.URL.Path, "/") {
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			return id
		}
	}

	return 0
}

func (f *fakeCloud) serverType() cloudObject {
	return cloudObject{
		keyID: 2, keyName: fakeMachineType, "architecture": "x86", "disk": fakeServerDisk, "cores": 2, "memory": 4,
	}
}

// lookup serves a lookup of one named object (location, server type, image).
func (f *fakeCloud) lookup(w http.ResponseWriter, r *http.Request, kind, list string, obj cloudObject) {
	if f.unknown[kind] || r.URL.Query().Get("name") != obj[keyName] {
		reply(w, cloudObject{list: []any{}})
		return
	}
	reply(w, cloudObject{list: []any{obj}})
}

func keyObject(k *cloudKey) cloudObject {
	return cloudObject{
		keyID: k.ID, keyName: k.Name, "fingerprint": k.Fingerprint, "public_key": k.PublicKey, keyLabels: k.Labels,
	}
}

func (f *fakeCloud) listKeys(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var out []any
	for _, k := range f.keys {
		if fp := q.Get("fingerprint"); fp != "" && fp != k.Fingerprint {
			continue
		}
		if !matchesSelector(q.Get("label_selector"), k.Labels) {
			continue
		}
		out = append(out, keyObject(k))
	}
	reply(w, cloudObject{"ssh_keys": nonNil(out)})
}

func (f *fakeCloud) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string            `json:"name"`
		PublicKey string            `json:"public_key"`
		Labels    map[string]string `json:"labels"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	fp, err := generateSSHKeyFingerprint(body.PublicKey)
	if err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "invalid_input", err.Error())
		return
	}
	k := &cloudKey{ID: f.id(), Name: body.Name, Fingerprint: fp, PublicKey: body.PublicKey, Labels: body.Labels}
	f.keys[k.ID] = k
	reply(w, cloudObject{"ssh_key": keyObject(k)})
}

// matchesSelector supports the only selector the provider uses: "key=value".
func matchesSelector(selector string, labels map[string]string) bool {
	if selector == "" {
		return true
	}
	key, value, _ := strings.Cut(selector, "=")

	return labels[key] == value
}

func nonNil(l []any) []any {
	if l == nil {
		return []any{}
	}

	return l
}

func (f *fakeCloud) serverObject(s *cloudServer) cloudObject {
	return cloudObject{
		keyID: s.ID, keyName: s.Name, keyStatus: s.Status, keyLabels: s.Labels,
		"public_net": cloudObject{"ipv4": cloudObject{"ip": fakeServerIP}},
	}
}

func (f *fakeCloud) listServers(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, s := range f.servers {
		if name := r.URL.Query().Get("name"); name != "" && name != s.Name {
			continue
		}
		out = append(out, f.serverObject(s))
	}
	reply(w, cloudObject{keyServers: nonNil(out)})
}

func (f *fakeCloud) getServer(w http.ResponseWriter, r *http.Request) {
	s := f.servers[pathID(r)]
	if s == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "server not found")
		return
	}
	reply(w, cloudObject{keyServer: f.serverObject(s)})
}

func (f *fakeCloud) createServer(w http.ResponseWriter, r *http.Request) {
	var body cloudObject
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.lastServerBody = body

	status := f.serverStatusOnCreate
	if status == "" {
		status = statusRunning
	}
	s := &cloudServer{ID: f.id(), Name: fmt.Sprint(body[keyName]), Status: status}
	s.Labels = map[string]string{}
	if labels, ok := body[keyLabels].(map[string]any); ok {
		for k, v := range labels {
			s.Labels[k] = fmt.Sprint(v)
		}
	}
	if vols, ok := body["volumes"].([]any); ok {
		for _, v := range vols {
			id := int64(v.(float64))
			s.Volumes = append(s.Volumes, id)
			if vol := f.volumes[id]; vol != nil {
				vol.Server = s.ID
			}
		}
	}
	f.servers[s.ID] = s
	reply(w, cloudObject{keyServer: f.serverObject(s), keyAction: fakeAction(), "next_actions": []any{}})
}

func (f *fakeCloud) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if f.servers[id] == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "server not found")
		return
	}
	delete(f.servers, id)
	for _, v := range f.volumes {
		if v.Server == id {
			v.Server = 0
		}
	}
	reply(w, cloudObject{keyAction: fakeAction()})
}

func (f *fakeCloud) setServerStatus(w http.ResponseWriter, r *http.Request, status string) {
	if s := f.servers[pathID(r)]; s != nil {
		s.Status = status
	}
	reply(w, cloudObject{keyAction: fakeAction()})
}

func (f *fakeCloud) volumeObject(v *cloudVolume) cloudObject {
	obj := cloudObject{keyID: v.ID, keyName: v.Name, "size": v.Size, keyLabels: v.Labels, keyServer: nil}
	if v.Server != 0 {
		obj[keyServer] = v.Server
	}

	return obj
}

func (f *fakeCloud) listVolumes(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, v := range f.volumes {
		if name := r.URL.Query().Get("name"); name != "" && name != v.Name {
			continue
		}
		out = append(out, f.volumeObject(v))
	}
	reply(w, cloudObject{"volumes": nonNil(out)})
}

func (f *fakeCloud) createVolume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string            `json:"name"`
		Size   int               `json:"size"`
		Labels map[string]string `json:"labels"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	v := &cloudVolume{ID: f.id(), Name: body.Name, Size: body.Size, Labels: body.Labels}
	f.volumes[v.ID] = v
	reply(w, cloudObject{"volume": f.volumeObject(v), keyAction: fakeAction(), "next_actions": []any{}})
}

func (f *fakeCloud) deleteVolume(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	v := f.volumes[id]
	switch {
	case v == nil:
		writeAPIError(w, http.StatusNotFound, "not_found", "volume not found")
	case v.Server != 0:
		writeAPIError(w, http.StatusConflict, "protected", "volume is attached")
	default:
		delete(f.volumes, id)
		w.WriteHeader(http.StatusNoContent)
	}
}
