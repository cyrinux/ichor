package ichorgo

import (
	"context"
	"strings"
	"testing"
)

const attachmentDetached = `{"metadata":{"name":"vol","namespace":"storage"},"spec":{"nodeID":""},"status":{"state":"detached","robustness":"healthy","currentNodeID":""}}`
const attachmentAttached = `{"metadata":{"name":"vol","namespace":"storage"},"spec":{"nodeID":"node-1"},"status":{"state":"attached","robustness":"healthy","currentNodeID":"node-1"}}`
const attachmentNode = `{"metadata":{"name":"node-1","namespace":"storage"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`

func attachmentAPI(t *testing.T, volume string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/longhorn.io/v1beta2/namespaces/storage/volumes/vol":                        volume,
		"GET /apis/longhorn.io/v1beta2/namespaces/storage/nodes/node-1":                       attachmentNode,
		"GET /apis/longhorn.io/v1beta2/namespaces/storage/nodes":                              `{"items":[` + attachmentNode + `]}`,
		"POST /api/v1/namespaces/storage/services/longhorn-backend:9500/proxy/v1/volumes/vol": `{}`,
	})
	k, e := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if e != nil {
		t.Fatal(e)
	}
	return f, k
}
func TestLonghornAttachmentPayload(t *testing.T) {
	for _, attach := range []bool{true, false} {
		v := attachmentAttached
		if attach {
			v = attachmentDetached
		}
		f, k := attachmentAPI(t, v)
		if err := changeLonghornAttachment(context.Background(), k, "storage", "vol", "node-1", attach); err != nil {
			t.Fatal(err)
		}
		r := lastRequest(f)
		if r.method != "POST" || !strings.Contains(r.body, `"hostId":"node-1"`) || !strings.Contains(r.body, `"attachmentID":""`) {
			t.Fatalf("%+v", r)
		}
		if attach {
			if r.query != "action=attach" || !strings.Contains(r.body, `"disableFrontend":false`) {
				t.Fatalf("%+v", r)
			}
		} else if r.query != "action=detach" || !strings.Contains(r.body, `"forceDetach":false`) {
			t.Fatalf("%+v", r)
		}
	}
}
func TestLonghornAttachmentRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, volume, node string
		attach             bool
	}{
		{"already attached", attachmentAttached, attachmentNode, true},
		{"already detached", attachmentDetached, attachmentNode, false},
		{"wrong node", strings.ReplaceAll(attachmentAttached, "node-1", "node-2"), attachmentNode, false},
		{"unready node", attachmentDetached, strings.Replace(attachmentNode, "True", "False", 1), true},
		{"faulted", strings.Replace(attachmentDetached, "healthy", "faulted", 1), attachmentNode, true},
		{"pending attach", strings.Replace(attachmentDetached, `"nodeID":""`, `"nodeID":"node-1"`, 1), attachmentNode, true},
		{"restoring", strings.Replace(attachmentDetached, `"state":"detached"`, `"restoreRequired":true,"state":"detached"`, 1), attachmentNode, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := attachmentAPI(t, tc.volume)
			f.answerWith("GET /apis/longhorn.io/v1beta2/namespaces/storage/nodes/node-1", 200, tc.node)
			if err := changeLonghornAttachment(context.Background(), k, "storage", "vol", "node-1", tc.attach); err == nil {
				t.Fatal("accepted invalid state")
			}
			if lastRequest(f).method == "POST" {
				t.Fatal("submitted after refusal")
			}
		})
	}
}
func TestLonghornAttachmentPreview(t *testing.T) {
	_, k := attachmentAPI(t, attachmentDetached)
	p, e := readLonghornAttachmentPreview(context.Background(), k, "storage", "vol")
	if e != nil || !p.CanAttach || len(p.Nodes) != 1 || !p.Nodes[0].Eligible || p.CanDetach {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestLonghornAttachmentErrorsAndDemo(t *testing.T) {
	f, k := attachmentAPI(t, attachmentDetached)
	f.answerWith("POST /api/v1/namespaces/storage/services/longhorn-backend:9500/proxy/v1/volumes/vol", 403, `{"kind":"Status"}`)
	if e := changeLonghornAttachment(context.Background(), k, "storage", "vol", "node-1", true); kubeCode(e) != 403 {
		t.Fatalf("%v", e)
	}
	f.answerWith("GET /apis", 200, `{"groups":[]}`)
	if e := changeLonghornAttachment(context.Background(), k, "storage", "vol", "node-1", true); e == nil {
		t.Fatal("missing accepted")
	}
	cfg, e := DemoConfig()
	if e != nil {
		t.Fatal(e)
	}
	if e := KubeLonghornAttachment(cfg, "Demo cluster", "", "longhorn-system", "pvc-5d1e", "demo-worker-3", true); e != errDemoUnavailable {
		t.Fatalf("%v", e)
	}
}
