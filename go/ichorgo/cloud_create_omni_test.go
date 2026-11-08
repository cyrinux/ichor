package ichorgo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"google.golang.org/grpc"
)

// fakeCloudMachine is a provider whose machine starts at once; prepare makes the real user data.
type fakeCloudMachine struct {
	join     *nodeJoin
	userData string
	created  bool
	deleted  bool
}

func (f *fakeCloudMachine) label() string            { return "Fake" }
func (f *fakeCloudMachine) machineWord() string      { return "machine" }
func (f *fakeCloudMachine) auditAction() auditAction { return auditAction{} }

func (f *fakeCloudMachine) prepare(_ context.Context, omni bool) (string, error) {
	data, err := cloudNodeUserData(f.join, omni, f.join.req, "")

	return data.UserData, err
}

func (f *fakeCloudMachine) create(_ context.Context, userData string) error {
	f.userData, f.created = userData, true

	return nil
}

func (f *fakeCloudMachine) waitRunning(context.Context) error     { return nil }
func (f *fakeCloudMachine) findUnanswered(context.Context, error) {}

func (f *fakeCloudMachine) machineID() string {
	if f.created {
		return "machine-1"
	}

	return ""
}

func (f *fakeCloudMachine) deleteMachine(context.Context) error {
	f.deleted = true

	return nil
}

func (f *fakeCloudMachine) transient(error) bool { return false }

// A cloud create into an Omni cluster with no machine yet makes the join config and creates the
// machine: only plain Talos needs the cluster's members (the host name check).
func TestCloudCreateOmniEmptyCluster(t *testing.T) {
	useMemAuthStore(t)

	key, err := pgp.GenerateKey("ichor", "test", testUserIdentity, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	f := &fakeOmniAPI{key: key, joinToken: "sample-token-id", joinConfig: sampleOmniJoinConfig}
	// The cluster lists no member through Omni's Talos proxy.
	f.store.types = map[string]map[string]*v1alpha1.Resource{"Members.cluster.talos.dev": {}}

	endpoint, caB64 := startTLSServer(t, func(s *grpc.Server) { v1alpha1.RegisterStateServer(s, f) }, grpc.UnknownServiceHandler(f.handle))
	f.endpoint = "https://" + endpoint

	cfg := omniTalosconfigYAML(testUserIdentity, f.endpoint, caB64)

	_, cfgCtx, err := resolveContext(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	armored, _ := key.Armor()
	kubeAuth.save(omniAuthKey(cfgCtx), kubeAuthState{Method: omniUserMethod, Session: map[string]string{omniKeySession: armored}})

	// The member listing itself answers "no machine", as Omni does for an empty cluster.
	if s, err := openSession(cfg, ""); err != nil {
		t.Fatal(err)
	} else {
		err := s.ready(context.Background())
		s.Close()

		if !errors.Is(err, errOmniNoMachines) {
			t.Fatalf("ready = %v, want errOmniNoMachines", err)
		}
	}

	rec := newJoinRecorder()
	join := &nodeJoin{
		config: cfg, listener: maskedJoinListener{rec}, interval: joinPollInterval,
		req: joinConfigRequest{role: "worker", hostname: "node-c"},
	}
	p := &fakeCloudMachine{join: join}

	omni := false
	run := &cloudCreateRun{
		provider: p, join: join, poll: 10 * time.Millisecond, omni: &omni,
		omniHostnames: func(context.Context, *session) ([]string, error) { return []string{"node-c"}, nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := run.run(ctx, time.Minute); err != nil {
		t.Fatalf("run = %v", err)
	}

	if !omni || !p.created || p.deleted || p.userData != sampleOmniJoinConfig {
		t.Errorf("omni %v, created %v, deleted %v, user data %q", omni, p.created, p.deleted, p.userData)
	}
}
