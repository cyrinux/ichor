package ichorgo

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"
)

func TestCloudJoinRequestLabels(t *testing.T) {
	bad := errors.New("invalid server request")

	for _, c := range []struct {
		name, labels string
		specErr      error
		want         map[string]string
		wantErr      string
	}{
		{name: "none", labels: ""},
		{name: "blank", labels: "  "},
		{name: "chosen", labels: `{"ichor/role":"worker","rack":"b2"}`, want: map[string]string{"ichor/role": "worker", "rack": "b2"}},
		{name: "omni's own prefix", labels: `{"omni.sidero.dev/cluster":"lab"}`, wantErr: "cannot be set"},
		{name: "not an object", labels: `["rack"]`, wantErr: "invalid labels"},
		{name: "spec error kept", labels: `{"omni.sidero.dev/x":"1"}`, specErr: bad, wantErr: bad.Error()},
	} {
		t.Run(c.name, func(t *testing.T) {
			req, err := cloudJoinRequest("node-a", "worker", "node-c", "/dev/sda", c.labels, c.specErr)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if req.template != "node-a" || req.role != "worker" || req.hostname != "node-c" || req.installDisk != "/dev/sda" {
				t.Errorf("req = %+v", req)
			}

			if !maps.Equal(req.labels, c.want) {
				t.Errorf("labels = %v, want %v", req.labels, c.want)
			}
		})
	}
}

func TestOmniCreateLabels(t *testing.T) {
	for _, c := range []struct {
		name, role string
		labels     map[string]string
		want       map[string]string
	}{
		{name: "no role, no label", want: nil},
		{name: "role folded in", role: "worker", want: map[string]string{"ichor/role": "worker"}},
		{name: "role next to the chosen labels", role: " controlplane ", labels: map[string]string{"rack": "b2"},
			want: map[string]string{"ichor/role": "controlplane", "rack": "b2"}},
		{name: "a chosen role label wins", role: "worker", labels: map[string]string{"ichor/role": "gpu"},
			want: map[string]string{"ichor/role": "gpu"}},
		{name: "unknown role ignored", role: "storage", labels: map[string]string{"rack": "b2"},
			want: map[string]string{"rack": "b2"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := maps.Clone(c.labels)

			got := omniCreateLabels(joinConfigRequest{role: c.role, labels: c.labels})
			if !maps.Equal(got, c.want) {
				t.Errorf("labels = %v, want %v", got, c.want)
			}

			if !maps.Equal(c.labels, before) {
				t.Errorf("the request's labels changed: %v", c.labels)
			}
		})
	}
}

// The Omni branch of every cloud create signs the role and the chosen labels into the join
// request (cloudNodeUserData's call, on a session to the fake Omni).
func TestCloudCreateOmniSignsLabels(t *testing.T) {
	useMemAuthStore(t)

	args := []string{"siderolink.api=https://omni.example.com:8090?jointoken=sample-secret-token"}
	f := &fakeOmniAPI{joinToken: "sample-token-id", joinArgs: args, joinConfig: sampleOmniJoinConfig}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	cfgCtx, err := omniInstanceContext(endpoint, testUserIdentity)
	if err != nil {
		t.Fatal(err)
	}

	s := &session{context: cfgCtx, signing: &omniSigning{method: omniMethodName(cfgCtx), signer: omniSigner{identity: testUserIdentity, key: f.key}}}

	req, err := cloudJoinRequest("", "worker", "node-c", "", `{"rack":"b2"}`, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data, err := omniCloudUserData(ctx, s, req.template, "", "", "", omniCreateLabels(req))
	if err != nil {
		t.Fatal(err)
	}

	if data.Kind != cloudUserDataOmni || data.UserData != sampleOmniJoinConfig {
		t.Errorf("data = %+v", data)
	}

	want := map[string]string{"ichor/role": "worker", "rack": "b2"}
	if !maps.Equal(f.joinLabels, want) {
		t.Errorf("labels in the join request = %v, want %v", f.joinLabels, want)
	}
}

// A label Omni keeps for itself stops every provider's create before any call.
func TestCloudCreatesRefuseReservedLabels(t *testing.T) {
	spec := `{"hostname":"node-c","labelsJson":"{\"omni.sidero.dev/cluster\":\"lab\"}"}`

	for name, start := range map[string]func(cfg, ctx, cred, spec string, l JoinListener) *JoinRun{
		"hetzner": StartHetznerCreate, "ovh": StartOVHCreate, "aws": StartAWSCreate,
		"azure": StartAzureCreate, "gcp": StartGCPCreate,
	} {
		t.Run(name, func(t *testing.T) {
			rec := newJoinRecorder()
			start(demoConfigForTest(t), "", "{}", spec, rec)

			if _, msg, _ := rec.wait(t); !strings.Contains(msg, "cannot be set") {
				t.Errorf("error = %q", msg)
			}
		})
	}
}
