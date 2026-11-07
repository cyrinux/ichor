package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const extSvc = `{"metadata":{"name":"shop","namespace":"demo-shop"},"spec":{"type":"ClusterIP"}}`

func extObjs(plural string, items ...string) externalObjects {
	var raws []json.RawMessage
	for _, i := range items {
		raws = append(raws, json.RawMessage(i))
	}

	return externalObjects{plural: raws}
}

// Each provider's matcher: the Service it is reached through, the name it carries, its health.
func TestExternalProviderMatchers(t *testing.T) {
	groups := map[string]string{groupNetBird: "v1alpha1", groupTwingate: "v1beta", groupNgrok: "v1alpha1", groupCloudflare: "v1alpha1"}

	router := `{"metadata":{"name":"edge","namespace":"demo-lab"},"spec":{"dnsZoneRef":{"name":"demo-lab.example"}}}`
	nbRes := func(ns, svc, extra string) string {
		return `{"metadata":{"name":"shop-res","namespace":"` + ns + `"},"spec":{"serviceRef":{"name":"` + svc + `"},"networkRouterRef":{"name":"edge","namespace":"demo-lab"}},"status":{` + extra + `}}`
	}

	tests := []struct {
		name    string
		kind    string
		svc     string
		objs    externalObjects
		want    []externalEntry // Provider, Name, URL, Health, Detail compared
		wantNil bool
	}{
		{name: "netbird resource with its router's zone", kind: tsKindService, svc: extSvc,
			objs: mergeObjs(extObjs("networkresources", nbRes("demo-shop", "shop", `"resourceID":"r1"`)), extObjs("networkrouters", router)),
			want: []externalEntry{{Provider: "NetBird", Kind: externalIngress, Name: "shop.demo-shop.demo-lab.example", Health: healthOK, Detail: "NetBird"}}},
		{name: "netbird resource without router", kind: tsKindService, svc: extSvc,
			objs: extObjs("networkresources", nbRes("demo-shop", "shop", `"resourceID":"r1"`)),
			want: []externalEntry{{Provider: "NetBird", Kind: externalIngress, Name: "demo-shop/shop-res", Health: healthOK, Detail: "NetBird"}}},
		{name: "netbird router of the same name in another namespace", kind: tsKindService, svc: extSvc,
			objs: mergeObjs(extObjs("networkresources", nbRes("demo-shop", "shop", `"resourceID":"r1"`)),
				extObjs("networkrouters", `{"metadata":{"name":"edge","namespace":"demo-shop"},"spec":{"dnsZoneRef":{"name":"other.example"}}}`)),
			want: []externalEntry{{Provider: "NetBird", Kind: externalIngress, Name: "demo-shop/shop-res", Health: healthOK, Detail: "NetBird"}}},
		{name: "netbird resource not published", kind: tsKindService, svc: extSvc,
			objs: extObjs("networkresources", nbRes("demo-shop", "shop", ``)),
			want: []externalEntry{{Provider: "NetBird", Kind: externalIngress, Name: "demo-shop/shop-res", Health: healthWarning, Detail: "NetBird · not published yet"}}},
		{name: "netbird failed condition", kind: tsKindService, svc: extSvc,
			objs: extObjs("networkresources", nbRes("demo-shop", "shop", `"resourceID":"r1","conditions":[{"type":"Ready","status":"False","reason":"APIError","message":"denied"}]`)),
			want: []externalEntry{{Provider: "NetBird", Kind: externalIngress, Name: "demo-shop/shop-res", Health: healthCritical, Detail: "NetBird · APIError: denied"}}},
		{name: "netbird other service", kind: tsKindService, svc: extSvc,
			objs: extObjs("networkresources", nbRes("demo-shop", "other", `"resourceID":"r1"`)), wantNil: true},
		{name: "netbird wrong namespace", kind: tsKindService, svc: extSvc,
			objs: extObjs("networkresources", nbRes("demo-lab", "shop", `"resourceID":"r1"`)), wantNil: true},
		{name: "netbird legacy resource by address", kind: tsKindService, svc: extSvc,
			objs: extObjs("nbresources", `{"metadata":{"name":"legacy","namespace":"demo-shop"},"spec":{"address":"shop.demo-shop.svc.cluster.local"},"status":{"networkResourceID":"r1"}}`),
			want: []externalEntry{{Provider: "NetBird", Kind: externalIngress, Name: "legacy", Health: healthOK, Detail: "NetBird"}}},
		{name: "twingate resource by address", kind: tsKindService, svc: extSvc,
			objs: extObjs("twingateresources", `{"metadata":{"name":"shop-resource","namespace":"demo-lab"},"spec":{"id":"abc","address":"shop.demo-shop.svc.cluster.local"}}`),
			want: []externalEntry{{Provider: "Twingate", Kind: externalIngress, Name: "shop.demo-shop.svc.cluster.local", Health: healthOK, Detail: "Twingate"}}},
		{name: "twingate alias wins, short address", kind: tsKindService, svc: extSvc,
			objs: extObjs("twingateresources", `{"metadata":{"name":"r"},"spec":{"id":"abc","address":"shop.demo-shop","alias":"shop.demo-lab.example"}}`),
			want: []externalEntry{{Provider: "Twingate", Kind: externalIngress, Name: "shop.demo-lab.example", Health: healthOK, Detail: "Twingate"}}},
		{name: "twingate not created", kind: tsKindService, svc: extSvc,
			objs: extObjs("twingateresources", `{"metadata":{"name":"r"},"spec":{"id":"","address":"shop.demo-shop.svc"}}`),
			want: []externalEntry{{Provider: "Twingate", Kind: externalIngress, Name: "shop.demo-shop.svc", Health: healthWarning, Detail: "Twingate · not created yet"}}},
		{name: "twingate wrong namespace", kind: tsKindService, svc: extSvc,
			objs: extObjs("twingateresources", `{"metadata":{"name":"r"},"spec":{"id":"abc","address":"shop.demo-lab.svc.cluster.local"}}`), wantNil: true},
		{name: "twingate outside address", kind: tsKindService, svc: extSvc,
			objs: extObjs("twingateresources", `{"metadata":{"name":"r"},"spec":{"id":"abc","address":"shop.demo-lab.example"}}`), wantNil: true},
		{name: "ngrok assigned url", kind: tsKindService, svc: extSvc,
			objs: extObjs("agentendpoints", `{"metadata":{"name":"e","namespace":"demo-shop"},"spec":{"url":"https://shop.example.invalid","upstream":{"url":"http://shop:8080"}},
			  "status":{"assignedURL":"https://shop-demo.example.invalid","conditions":[{"type":"Ready","status":"True"}]}}`),
			want: []externalEntry{{Provider: "ngrok", Kind: externalIngress, Name: "https://shop-demo.example.invalid", URL: "https://shop-demo.example.invalid", Health: healthOK, Detail: "ngrok"}}},
		{name: "ngrok upstream names its namespace", kind: tsKindService, svc: extSvc,
			objs: extObjs("agentendpoints", `{"metadata":{"name":"e","namespace":"demo-lab"},"spec":{"url":"https://shop.example.invalid","upstream":{"url":"http://shop.demo-shop.svc.cluster.local:80"}}}`),
			want: []externalEntry{{Provider: "ngrok", Kind: externalIngress, Name: "https://shop.example.invalid", URL: "https://shop.example.invalid", Health: healthOK, Detail: "ngrok"}}},
		{name: "ngrok upstream error", kind: tsKindService, svc: extSvc,
			objs: extObjs("agentendpoints", `{"metadata":{"name":"e","namespace":"demo-shop"},"spec":{"url":"tcp://1.tcp.example.invalid:1234","upstream":{"url":"tcp://shop:22"}},
			  "status":{"conditions":[{"type":"EndpointCreated","status":"False","reason":"UpstreamError","message":"refused"}]}}`),
			want: []externalEntry{{Provider: "ngrok", Kind: externalIngress, Name: "tcp://1.tcp.example.invalid:1234", Health: healthCritical, Detail: "ngrok · UpstreamError: refused"}}},
		{name: "ngrok pending", kind: tsKindService, svc: extSvc,
			objs: extObjs("agentendpoints", `{"metadata":{"name":"e","namespace":"demo-shop"},"spec":{"url":"https://shop.example.invalid","upstream":{"url":"http://shop"}},
			  "status":{"conditions":[{"type":"Ready","status":"False","reason":"Pending"}]}}`),
			want: []externalEntry{{Provider: "ngrok", Kind: externalIngress, Name: "https://shop.example.invalid", URL: "https://shop.example.invalid", Health: healthWarning, Detail: "ngrok · Pending"}}},
		{name: "ngrok wrong namespace", kind: tsKindService, svc: extSvc,
			objs: extObjs("agentendpoints", `{"metadata":{"name":"e","namespace":"demo-lab"},"spec":{"url":"https://x.example.invalid","upstream":{"url":"http://shop:80"}}}`), wantNil: true},
		{name: "ngrok outside upstream", kind: tsKindService, svc: extSvc,
			objs: extObjs("agentendpoints", `{"metadata":{"name":"e","namespace":"demo-shop"},"spec":{"url":"https://x.example.invalid","upstream":{"url":"http://shop.example.com"}}}`), wantNil: true},
		{name: "cloudflare binding hostname, by the subject's index", kind: tsKindService, svc: extSvc,
			objs: extObjs("tunnelbindings", `{"metadata":{"name":"b","namespace":"demo-shop"},
			  "subjects":[{"kind":"Service","name":"cart"},{"kind":"Service","name":"shop","spec":{"fqdn":"shop.demo-lab.example"}}],
			  "status":{"hostnames":"cart.demo-lab.example,shop.demo-lab.example","services":[{"hostname":"cart.demo-lab.example","target":"http://cart.demo-shop.svc:80"},{"hostname":"shop.demo-lab.example","target":"http://shop.demo-shop.svc:80"}]}}`),
			want: []externalEntry{{Provider: "Cloudflare", Kind: externalIngress, Name: "shop.demo-lab.example", URL: "https://shop.demo-lab.example", Health: healthOK, Detail: "Cloudflare"}}},
		{name: "cloudflare binding not published, by its fqdn", kind: tsKindService, svc: extSvc,
			objs: extObjs("tunnelbindings", `{"metadata":{"name":"b","namespace":"demo-shop"},"subjects":[{"name":"shop","spec":{"fqdn":"shop.demo-lab.example"}}]}`),
			want: []externalEntry{{Provider: "Cloudflare", Kind: externalIngress, Name: "shop.demo-lab.example", Health: healthWarning, Detail: "Cloudflare · not published yet"}}},
		{name: "cloudflare binding not published", kind: tsKindService, svc: extSvc,
			objs: extObjs("tunnelbindings", `{"metadata":{"name":"b","namespace":"demo-shop"},"subjects":[{"name":"shop"}]}`),
			want: []externalEntry{{Provider: "Cloudflare", Kind: externalIngress, Name: "demo-shop/b", Health: healthWarning, Detail: "Cloudflare · not published yet"}}},
		{name: "cloudflare wrong namespace", kind: tsKindService, svc: extSvc,
			objs: extObjs("tunnelbindings", `{"metadata":{"name":"b","namespace":"demo-lab"},"subjects":[{"name":"shop"}]}`), wantNil: true},
		{name: "cloudflare other subject", kind: tsKindService, svc: extSvc,
			objs: extObjs("tunnelbindings", `{"metadata":{"name":"b","namespace":"demo-shop"},"subjects":[{"name":"other"}]}`), wantNil: true},
		{name: "an ingress has none", kind: tsKindIngress, svc: extSvc,
			objs: extObjs("networkresources", nbRes("demo-shop", "shop", `"resourceID":"r1"`)), wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := externalEntries(tt.kind, json.RawMessage(tt.svc), groups, tt.objs)
			if tt.wantNil {
				if len(got) != 0 {
					t.Fatalf("got %+v", got)
				}

				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %+v", got)
			}

			for i, w := range tt.want {
				g := got[i]
				if g.Provider != w.Provider || g.Kind != w.Kind || g.Name != w.Name || g.URL != w.URL || g.Health != w.Health || g.Detail != w.Detail {
					t.Errorf("entry %d\n got %+v\nwant %+v", i, g, w)
				}
			}
		})
	}
}

// A group the cluster does not serve: its objects are not even looked at.
func TestExternalEntriesGate(t *testing.T) {
	objs := extObjs("twingateresources", `{"metadata":{"name":"r"},"spec":{"id":"abc","address":"shop.demo-shop.svc"}}`)

	if got := externalEntries(tsKindService, json.RawMessage(extSvc), map[string]string{groupNetBird: "v1alpha1"}, objs); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// No provider group served: no list call at all. One served: only its own lists, the legacy
// NetBird kind at its own version, a missing one not an error.
func TestReadExternalObjects(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	opened := len(f.recorded()) // openKubeClient's own /version

	objs, err := readExternalObjects(context.Background(), k, map[string]string{groupTailscale: "v1alpha1", "argoproj.io": "v1alpha1"})
	if err != nil || len(objs) != 0 || len(f.recorded()) != opened {
		t.Fatalf("objs %v err %v requests %v", objs, err, f.recorded()[opened:])
	}

	f = newFakeKubeAPI(t, map[string]string{
		"GET /apis/netbird.io/v1alpha1/networkresources": `{"items":[{"metadata":{"name":"a"}}]}`,
		"GET /apis/netbird.io/v1alpha1/networkrouters":   `{"items":[]}`,
	})

	if k, err = openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, ""); err != nil {
		t.Fatal(err)
	}

	objs, err = readExternalObjects(context.Background(), k, map[string]string{groupNetBird: "v1alpha1"})
	if err != nil || len(objs["networkresources"]) != 1 {
		t.Fatalf("objs %v err %v", objs, err)
	}

	for _, r := range f.recorded() {
		if r.path != "/version" && !strings.HasPrefix(r.path, "/apis/netbird.io/") {
			t.Fatalf("request %s", r.path)
		}
	}
}

func mergeObjs(all ...externalObjects) externalObjects {
	out := externalObjects{}

	for _, o := range all {
		for k, v := range o {
			out[k] = v
		}
	}

	return out
}
