package ichorgo

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"golang.org/x/crypto/ssh"
)

const (
	snapKeyID    = int64(31)
	snapServerID = int64(32)
	snapImageID  = int64(33)
	snapAddress  = "192.0.2.30"
)

// fakeHetznerImage is the API of a project as a snapshot run sees it, and the rescue system's SSH
// server. It records every write and every command.
type fakeHetznerImage struct {
	mu sync.Mutex

	readOnly       bool
	neverOff       bool
	failServerPost int // the status POST /servers answers, after creating
	failImage      bool
	failDelete     bool
	failWget       bool
	lockedDeletes  int
	onWrite        func() // blocks the write: Stop's tests

	authorized  string
	posts       map[string]map[string]any
	deleted     []string
	commands    []string
	status      string
	serverExist bool
	keyExist    bool
	imageExist  bool
	dials       []string

	sshListener net.Listener
}

func startFakeHetznerImage(t *testing.T, f *fakeHetznerImage) {
	t.Helper()

	f.posts = map[string]map[string]any{}
	f.status = "initializing"

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	client, endpoint := cloudHTTP, hetznerEndpoint
	cloudHTTP = func() *http.Client { return srv.Client() }
	hetznerEndpoint = srv.URL

	t.Cleanup(func() { cloudHTTP, hetznerEndpoint = client, endpoint })

	f.startSSH(t)
}

func (f *fakeHetznerImage) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer sample-hetzner-token" {
		hetznerFail(w, http.StatusUnauthorized, "unauthorized", "unable to authenticate")

		return
	}

	if r.Method == http.MethodPost {
		f.post(w, r)

		return
	}

	if r.Method == http.MethodDelete {
		f.delete(w, r)

		return
	}

	switch r.URL.Path {
	case "/pricing":
		fmt.Fprint(w, `{"pricing":{"currency":"EUR","image":{"price_per_gb_month":{"net":"0.0119000000","gross":"0.0142"}}}}`)
	case "/server_types":
		fmt.Fprint(w, snapTypes)
	case fmt.Sprintf("/servers/%d", snapServerID):
		fmt.Fprintf(w, `{"server":{"id":%d,"status":%q,"public_net":{"ipv4":{"ip":%q}}}}`, snapServerID, f.status, snapAddress)
	case "/servers":
		f.listByLabel(w, "servers", f.serverExist, snapServerID)
	case "/ssh_keys":
		f.listByLabel(w, "ssh_keys", f.keyExist, snapKeyID)
	case fmt.Sprintf("/images/%d", snapImageID):
		fmt.Fprintf(w, `{"image":{"id":%d,"image_size":1.5,"created":"2026-10-31T09:00:00+00:00"}}`, snapImageID)
	default:
		if strings.HasPrefix(r.URL.Path, "/actions/") {
			f.action(w, r)

			return
		}

		hetznerFail(w, http.StatusNotFound, "not_found", r.URL.Path)
	}
}

const snapTypes = `{"server_types":[
	{"name":"cx33","architecture":"x86","deprecation":null,"prices":[{"location":"fsn1","price_hourly":{"net":"0.0090"},"price_monthly":{"net":"5.49"}}]},
	{"name":"cx23","architecture":"x86","deprecation":null,"prices":[{"location":"fsn1","price_hourly":{"net":"0.0060"},"price_monthly":{"net":"3.49"}}]},
	{"name":"cax11","architecture":"arm","deprecation":null,"prices":[{"location":"hel1","price_hourly":{"net":"0.0070"},"price_monthly":{"net":"3.79"}}]}
],"meta":{"pagination":{"next_page":null}}}`

func (f *fakeHetznerImage) listByLabel(w http.ResponseWriter, key string, exists bool, id int64) {
	items := ""
	if exists {
		items = fmt.Sprintf(`{"id":%d}`, id)
	}

	fmt.Fprintf(w, `{%q:[%s],"meta":{"pagination":{"next_page":null}}}`, key, items)
}

func (f *fakeHetznerImage) action(w http.ResponseWriter, r *http.Request) {
	status := "success"
	if r.URL.Path == "/actions/105" && f.failImage {
		status = "error"
	}

	fmt.Fprintf(w, `{"action":{"id":1,"status":%q,"error":{"message":"no capacity"}}}`, status)
}

func (f *fakeHetznerImage) post(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)

	var body map[string]any
	_ = json.Unmarshal(data, &body)

	if f.readOnly {
		hetznerFail(w, http.StatusForbidden, "forbidden", "insufficient permissions")

		return
	}

	switch r.URL.Path {
	case "/ssh_keys":
		if body["name"] == nil {
			hetznerFail(w, http.StatusUnprocessableEntity, "invalid_input", "name is required")

			return
		}

		f.posts[r.URL.Path] = body
		f.authorized, _ = body["public_key"].(string)
		f.keyExist = true
		fmt.Fprintf(w, `{"ssh_key":{"id":%d}}`, snapKeyID)
	case "/servers":
		f.posts[r.URL.Path] = body
		f.serverExist, f.status = true, "off"

		if f.failServerPost != 0 {
			hetznerFail(w, f.failServerPost, "server_error", "something broke")

			return
		}

		fmt.Fprintf(w, `{"server":{"id":%d,"status":"off"},"action":{"id":101,"status":"running"}}`, snapServerID)
	default:
		f.post2(w, r.URL.Path, body)
	}
}

func (f *fakeHetznerImage) post2(w http.ResponseWriter, path string, body map[string]any) {
	f.posts[path] = body

	prefix := fmt.Sprintf("/servers/%d/actions/", snapServerID)

	switch strings.TrimPrefix(path, prefix) {
	case "enable_rescue":
		fmt.Fprint(w, `{"root_password":"never-shown","action":{"id":102,"status":"running"}}`)
	case "poweron":
		f.status = hetznerStatusRunning
		fmt.Fprint(w, `{"action":{"id":103,"status":"running"}}`)
	case "poweroff":
		f.status = hetznerStatusOff
		fmt.Fprint(w, `{"action":{"id":104,"status":"running"}}`)
	case "create_image":
		f.imageExist = true
		fmt.Fprintf(w, `{"image":{"id":%d},"action":{"id":105,"status":"running"}}`, snapImageID)
	default:
		hetznerFail(w, http.StatusNotFound, "not_found", path)
	}
}

func (f *fakeHetznerImage) delete(w http.ResponseWriter, r *http.Request) {
	if f.failDelete {
		hetznerFail(w, http.StatusForbidden, "forbidden_delete", "not allowed")

		return
	}

	if f.lockedDeletes > 0 {
		f.lockedDeletes--
		hetznerFail(w, http.StatusLocked, "locked", "an action is running")

		return
	}

	f.deleted = append(f.deleted, r.URL.Path)

	switch r.URL.Path {
	case fmt.Sprintf("/servers/%d", snapServerID):
		f.serverExist = false
	case fmt.Sprintf("/ssh_keys/%d", snapKeyID):
		f.keyExist = false
	case fmt.Sprintf("/images/%d", snapImageID):
		f.imageExist = false
	}

	fmt.Fprint(w, `{}`)
}

func (f *fakeHetznerImage) startSSH(t *testing.T) {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	hostKey, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}

	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		f.mu.Lock()
		defer f.mu.Unlock()

		if strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) != f.authorized {
			return nil, errors.New("unknown key")
		}

		return &ssh.Permissions{}, nil
	}}
	config.AddHostKey(hostKey)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	f.sshListener = listener
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go f.serveSSH(conn, config)
		}
	}()
}

func (f *fakeHetznerImage) serveSSH(conn net.Conn, config *ssh.ServerConfig) {
	sshConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close() //nolint:errcheck

	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		channel, reqs, err := newChannel.Accept()
		if err != nil {
			return
		}

		go f.serveSession(channel, reqs)
	}
}

func (f *fakeHetznerImage) serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close() //nolint:errcheck

	for req := range requests {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)

			continue
		}

		var exec struct{ Command string }
		_ = ssh.Unmarshal(req.Payload, &exec)
		_ = req.Reply(true, nil)

		code := f.runCommand(exec.Command, channel)
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))

		return
	}
}

func (f *fakeHetznerImage) runCommand(command string, channel ssh.Channel) uint32 {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	failWget, onWrite, neverOff := f.failWget, f.onWrite, f.neverOff
	f.mu.Unlock()

	switch {
	case strings.Contains(command, "wget"):
		if onWrite != nil {
			onWrite()
		}

		if failWget {
			fmt.Fprint(channel.Stderr(), "wget: server returned error: HTTP/1.1 404 Not Found\n")

			return 8
		}

		fmt.Fprint(channel.Stderr(), "1048576 bytes copied, 1 s\r2097152 bytes copied, 2 s\n")
	case strings.HasPrefix(command, "shutdown") && !neverOff:
		f.mu.Lock()
		f.status = hetznerStatusOff
		f.mu.Unlock()
	}

	return 0
}

func (f *fakeHetznerImage) snapshotRun(cfg string, rec *joinRecorder) *hetznerSnapshotRun {
	return &hetznerSnapshotRun{
		config: cfg, context: "fake", tokenJSON: hetznerTestToken,
		spec:     hetznerSnapshotSpec{TemplateNode: joinTemplateNode, Location: "fsn1"},
		listener: maskedJoinListener{rec}, poll: joinPollInterval, offWait: 5 * joinPollInterval,
		ssh: hetznerSSH{
			dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				f.mu.Lock()
				f.dials = append(f.dials, address)
				f.mu.Unlock()

				return (&net.Dialer{}).DialContext(ctx, network, f.sshListener.Addr().String())
			},
			bootWait: time.Second, retry: joinPollInterval,
		},
	}
}

// gone says the project holds nothing the run made.
func (f *fakeHetznerImage) gone(t *testing.T) {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.serverExist || f.keyExist {
		t.Errorf("left behind: server %v, key %v (deleted %v)", f.serverExist, f.keyExist, f.deleted)
	}
}

func TestHetznerSnapshotHappyPath(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeHetznerImage{}
	startFakeHetznerImage(t, f)

	rec := newJoinRecorder()
	f.snapshotRun(cfg, rec).start(time.Minute)

	result, msg, events := rec.wait(t)
	if msg != "" {
		t.Fatalf("error %q, events %+v", msg, events)
	}

	want := []string{hetznerPhaseKey, hetznerPhaseCreating, hetznerPhaseRescue, hetznerPhaseWriting, hetznerPhaseSnapshot, hetznerPhaseCleanup, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	var r hetznerSnapshotResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}

	if r.ImageID != snapImageID || r.SizeGB != 1.5 || r.Created == "" || r.Labels["os"] != "talos" || r.Labels["talos-version"] != "v1.14.2" ||
		r.Labels["arch"] != bootArchAMD64 || !strings.Contains(r.Description, "Talos v1.14.2 hcloud amd64") {
		t.Errorf("result = %+v", r)
	}

	for key, value := range r.Labels {
		if len(value) > 63 {
			t.Errorf("label %s has %d characters", key, len(value))
		}
	}

	f.gone(t)
	f.checkHappyWrites(t)

	if !f.imageExist || slices.Contains(f.deleted, fmt.Sprintf("/images/%d", snapImageID)) {
		t.Errorf("the finished snapshot was deleted: %v", f.deleted)
	}

	if !slices.Contains(f.dials, net.JoinHostPort(snapAddress, "22")) {
		t.Errorf("dials = %v", f.dials)
	}

	for _, e := range events {
		if strings.Contains(e.Message, "never-shown") || strings.Contains(e.Message, "sample-hetzner-token") {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", hetznerAuditAction)
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "arch=amd64 location=fsn1 version=v1.14.2") {
		t.Errorf("audit = %+v", entries)
	}
}

func (f *fakeHetznerImage) checkHappyWrites(t *testing.T) {
	t.Helper()

	server := f.posts["/servers"]
	if server["server_type"] != "cx23" || server["location"] != "fsn1" || server["image"] != hetznerStockImage || server["start_after_create"] != false {
		t.Errorf("POST /servers = %+v (cx23 is the cheapest x86 type at fsn1)", server)
	}

	rescue := f.posts[fmt.Sprintf("/servers/%d/actions/enable_rescue", snapServerID)]
	if rescue["type"] != hetznerRescueType {
		t.Errorf("enable_rescue = %+v", rescue)
	}

	image := f.posts[fmt.Sprintf("/servers/%d/actions/create_image", snapServerID)]
	if image["type"] != hetznerImageType {
		t.Errorf("create_image = %+v", image)
	}

	if len(f.commands) != 3 || !strings.HasPrefix(f.commands[0], "blkdiscard --force /dev/sda") || !strings.HasPrefix(f.commands[2], "shutdown") {
		t.Fatalf("commands = %q", f.commands)
	}

	write := f.commands[1]
	if !strings.Contains(write, "pipefail") || !strings.Contains(write, "/v1.14.2/hcloud-amd64.raw.xz") ||
		!strings.Contains(write, "xz -cd | dd of=/dev/sda bs=4M conv=sparse") || !strings.Contains(write, "&& sync") {
		t.Errorf("write command = %q", write)
	}
}

func TestHetznerSnapshotFailures(t *testing.T) {
	cases := []struct {
		name        string
		fake        *fakeHetznerImage
		wantInError []string
	}{
		{
			name: "the download fails: server and key deleted", fake: &fakeHetznerImage{failWget: true},
			wantInError: []string{"404 Not Found", "Deleted", "server 32", "SSH key 31"},
		},
		{
			name: "the snapshot fails: the cleanup still runs", fake: &fakeHetznerImage{failImage: true},
			wantInError: []string{"no capacity", "server 32", "SSH key 31", "unfinished snapshot 33"},
		},
		{
			name: "POST /servers without an answer: found by its label", fake: &fakeHetznerImage{failServerPost: http.StatusBadGateway},
			wantInError: []string{"something broke", "server 32", "SSH key 31"},
		},
		{
			name: "a locked server is deleted once the action ended", fake: &fakeHetznerImage{failWget: true, lockedDeletes: 2},
			wantInError: []string{"404 Not Found", "server 32", "SSH key 31"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeHetznerImage(t, c.fake)

			rec := newJoinRecorder()
			c.fake.snapshotRun(cfg, rec).start(time.Minute)

			result, msg, _ := rec.wait(t)
			if result != "" {
				t.Errorf("result = %q", result)
			}

			for _, want := range c.wantInError {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want %q in it", msg, want)
				}
			}

			c.fake.gone(t)

			if entries := readAudit(t, "fake", hetznerAuditAction); len(entries) != 1 || entries[0].Outcome == auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestHetznerSnapshotRefusedBeforeAnyWrite(t *testing.T) {
	cases := []struct {
		name string
		fake *fakeHetznerImage
		edit func(*hetznerSnapshotRun)
		want string
	}{
		{name: "read-only token", fake: &fakeHetznerImage{readOnly: true}, want: "read-only"},
		{name: "unknown architecture", fake: &fakeHetznerImage{}, edit: func(r *hetznerSnapshotRun) { r.spec.Arch = "riscv64" }, want: "unknown architecture"},
		{name: "no location", fake: &fakeHetznerImage{}, edit: func(r *hetznerSnapshotRun) { r.spec.Location = " " }, want: "pick a location"},
		{name: "no type of the architecture there", fake: &fakeHetznerImage{}, edit: func(r *hetznerSnapshotRun) { r.spec.Arch = bootArchARM64 }, want: "no server type for arm64 at fsn1"},
		{name: "a member that cannot be read", fake: &fakeHetznerImage{}, edit: func(r *hetznerSnapshotRun) { r.spec.TemplateNode = "192.0.2.99" }, want: "192.0.2.99"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeHetznerImage(t, c.fake)

			rec := newJoinRecorder()
			run := c.fake.snapshotRun(cfg, rec)

			if c.edit != nil {
				c.edit(run)
			}

			run.start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.want) {
				t.Errorf("error = %q, want %q in it", msg, c.want)
			}

			if len(c.fake.posts) != 0 {
				t.Errorf("writes = %+v", c.fake.posts)
			}
		})
	}
}

// Without the template's config the schematic is unknown: no snapshot of the wrong image.
func TestHetznerSnapshotRefusesAnUnreadableSchematic(t *testing.T) {
	withDataDir(t)

	talos := newFakeTalos()
	talos.addNode(t, joinTemplateNode, "v1.14.2", machine.TypeWorker)
	cfg := talos.start(t, joinTemplateNode)

	f := &fakeHetznerImage{}
	startFakeHetznerImage(t, f)

	rec := newJoinRecorder()
	f.snapshotRun(cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "schematic") || len(f.posts) != 0 {
		t.Errorf("error = %q, writes = %v", msg, f.posts)
	}
}

func TestHetznerSnapshotNeverOffPowersOff(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeHetznerImage{neverOff: true}
	startFakeHetznerImage(t, f)

	rec := newJoinRecorder()
	f.snapshotRun(cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); msg != "" {
		t.Fatalf("error %q", msg)
	}

	if _, ok := f.posts[fmt.Sprintf("/servers/%d/actions/poweroff", snapServerID)]; !ok {
		t.Errorf("no poweroff: %v", f.posts)
	}

	if _, ok := f.posts[fmt.Sprintf("/servers/%d/actions/create_image", snapServerID)]; !ok {
		t.Errorf("no image after the poweroff: %v", f.posts)
	}
}

func TestHetznerSnapshotStopCleansUp(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	writing := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	f := &fakeHetznerImage{onWrite: func() {
		close(writing)
		<-release
	}}
	startFakeHetznerImage(t, f)

	rec := newJoinRecorder()
	run := f.snapshotRun(cfg, rec).start(time.Minute)

	<-writing
	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "server 32") || !strings.Contains(msg, "SSH key 31") {
		t.Errorf("error = %q", msg)
	}

	f.gone(t)
}

func TestHetznerSnapshotFailedCleanupNamesTheServer(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeHetznerImage{failDelete: true}
	startFakeHetznerImage(t, f)

	rec := newJoinRecorder()
	f.snapshotRun(cfg, rec).start(time.Minute)

	_, msg, _ := rec.wait(t)
	for _, want := range []string{"snapshot 33 is ready", "server 32 (ichor-snap-", "still billed", "Hetzner console"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want %q in it", msg, want)
		}
	}
}

func TestHetznerSnapshotDemoRefused(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := newJoinRecorder()
	StartHetznerSnapshot(cfg, "", "", `{"location":"fsn1"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo snapshot = %q", msg)
	}
}

func TestHetznerSnapshotEstimate(t *testing.T) {
	f := &fakeHetznerImage{}
	startFakeHetznerImage(t, f)

	out, err := HetznerSnapshotEstimate(hetznerTestToken, bootArchAMD64, "fsn1")
	if c := decodeJSON[hetznerSnapshotCost](t, out, err); c.ServerType != "cx23" || c.Hourly != "0.0060" || c.MonthlyPerGB != "0.0119" || c.Currency != "EUR" {
		t.Errorf("estimate = %+v", c)
	}

	if _, err := HetznerSnapshotEstimate(hetznerTestToken, bootArchARM64, "fsn1"); err == nil || !strings.Contains(err.Error(), "no server type") {
		t.Errorf("arm64 at fsn1: %v", err)
	}

	if len(f.posts) != 0 {
		t.Errorf("an estimate wrote: %v", f.posts)
	}
}

func TestHostKeyPin(t *testing.T) {
	key := func() ssh.PublicKey {
		public, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		k, err := ssh.NewPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}

		return k
	}

	pin, first, other := &hostKeyPin{}, key(), key()

	if err := pin.check("", nil, first); err != nil {
		t.Errorf("first key: %v", err)
	}

	if err := pin.check("", nil, first); err != nil {
		t.Errorf("same key: %v", err)
	}

	if err := pin.check("", nil, other); !errors.Is(err, errHetznerHostKeyChanged) {
		t.Errorf("other key: %v", err)
	}
}

func TestHetznerOutputTail(t *testing.T) {
	var lines []string

	tail := &hetznerOutputTail{every: time.Hour, emit: func(l string) { lines = append(lines, l) }}

	fmt.Fprint(tail, "first\rsecond\n\nthi")
	fmt.Fprint(tail, "rd")
	tail.flush()

	if !slices.Equal(lines, []string{"first", "third"}) || tail.lastLine() != "third" {
		t.Errorf("lines = %q, last = %q", lines, tail.lastLine())
	}
}
