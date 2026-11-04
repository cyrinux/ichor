package ichorgo

import (
	"encoding/json"
	"strconv"

	"github.com/cyrinux/ichor/go/kube/netmap"
)

// demoTailscaleNetwork is the map of one of the demo's exposed rows (demoTailscaleExposed),
// built by the same code as a real one from demo objects: a Service on a healthy ProxyGroup
// spread over two zones, one naming a missing ProxyGroup, a Service and an Ingress with proxies
// of their own (the Ingress's crash-loops), an egress to a tailnet database and one whose proxy
// was never provisioned. Anything else has an empty map.
func demoTailscaleNetwork(kind, namespace, name string) netmap.Map {
	in := tsNetInput{
		kind:  kind,
		nodes: demoNetNodes(),
		zones: map[string]string{"demo-worker-1": "zone-a", "demo-worker-2": "zone-b", "demo-worker-3": "zone-c"},
		groups: []tsProxyGroupObject{demoNetObject[tsProxyGroupObject](`{"metadata":{"name":"ingress-proxies"},
		  "spec":{"type":"ingress","replicas":2,"hostnamePrefix":"ingress","tags":["tag:k8s"]},
		  "status":{"conditions":[{"type":"ProxyGroupReady","status":"True"},{"type":"ProxyGroupAvailable","status":"True"}],
		    "devices":[{"hostname":"ingress-0","tailnetIPs":["100.64.0.31"]},{"hostname":"ingress-1","tailnetIPs":["100.64.0.32"]}]}}`)},
	}

	pod := func(ns, name, node, status string, restarts int, labels map[string]string) netmap.Pod {
		p := kubePod{Namespace: ns, Name: name, Node: node, Status: status, Containers: 1, Restarts: restarts}
		if status == "Running" {
			p.Ready, p.Healthy = 1, true
		}

		return netmap.Pod{Pod: p, Labels: labels}
	}
	backend := func(ns, app string, nodes ...string) {
		in.services = append(in.services, demoNetObject[netmap.Service](`{"metadata":{"name":"`+app+`","namespace":"`+ns+`"},
		  "spec":{"type":"ClusterIP","clusterIP":"10.96.60.`+strconv.Itoa(10+len(in.services))+`","selector":{"app":"`+app+`"},"ports":[{"port":80,"targetPort":8080}]}}`))
		for i, node := range nodes {
			in.pods = append(in.pods, pod(ns, app+"-7f9c4-"+[]string{"h2kq8", "m5xw3"}[i], node, "Running", 0, map[string]string{"app": app}))
		}
	}
	proxy := func(name, node, status string, restarts int, parentType, parent, parentNS string) {
		in.proxies = append(in.proxies, pod("tailscale", name, node, status, restarts, map[string]string{
			tsLabelManaged: "true", tsLabelParentType: parentType, tsLabelParent: parent, tsLabelParentNS: parentNS,
		}))
	}

	switch kind + "/" + namespace + "/" + name {
	case "Service/shop/storefront":
		in.object = demoTSService(`"name":"storefront","namespace":"shop","annotations":{"tailscale.com/proxy-group":"ingress-proxies","tailscale.com/tags":"tag:k8s"}`,
			`"type":"LoadBalancer","loadBalancerClass":"tailscale","ports":[{"port":443},{"port":80}]`,
			`"loadBalancer":{"ingress":[{"hostname":"storefront.example-tailnet.ts.net","ip":"100.64.0.51"}]},
			 "conditions":[{"type":"TailscaleIngressSvcValid","status":"True"},{"type":"TailscaleIngressSvcConfigured","status":"True"}]`)
		backend("shop", "storefront", "demo-worker-1", "demo-worker-2")
		proxy("ingress-proxies-0", "demo-worker-1", "Running", 0, tsParentGroup, "ingress-proxies", "")
		proxy("ingress-proxies-1", "demo-worker-2", "Running", 0, tsParentGroup, "ingress-proxies", "")
	case "Service/media/player":
		in.object = demoTSService(`"name":"player","namespace":"media","annotations":{"tailscale.com/proxy-group":"web-proxies"}`,
			`"type":"LoadBalancer","loadBalancerClass":"tailscale","ports":[{"port":8096}]`,
			`"loadBalancer":{"ingress":[{"hostname":"player.example-tailnet.ts.net"}]},
			 "conditions":[{"type":"TailscaleIngressSvcConfigured","status":"True"}]`)
		backend("media", "player", "demo-worker-2")
	case "Service/monitoring/grafana":
		in.object = demoTSService(`"name":"grafana","namespace":"monitoring","annotations":{"tailscale.com/expose":"true","tailscale.com/hostname":"grafana"}`,
			`"type":"ClusterIP","ports":[{"port":3000}]`, `"conditions":[{"type":"TailscaleProxyReady","status":"True"}]`)
		backend("monitoring", "grafana", "demo-worker-2")
		proxy("ts-grafana-x7k2p-0", "demo-worker-1", "Running", 0, "svc", "grafana", "monitoring")
	case "Ingress/docs/wiki":
		in.object = json.RawMessage(`{"metadata":{"name":"wiki","namespace":"docs","annotations":{"tailscale.com/tags":"tag:k8s"}},
		  "spec":{"ingressClassName":"tailscale","defaultBackend":{"service":{"name":"wiki","port":{"number":80}}},"tls":[{"hosts":["wiki"]}]},
		  "status":{"loadBalancer":{"ingress":[{"hostname":"wiki.example-tailnet.ts.net"}]}}}`)
		backend("docs", "wiki", "demo-worker-1", "demo-worker-2")
		proxy("ts-wiki-q9d4m-0", "demo-worker-1", "CrashLoopBackOff", 23, "ingress", "wiki", "docs")
	case "Service/payments/ledger-db":
		in.object = demoTSService(`"name":"ledger-db","namespace":"payments","annotations":{"tailscale.com/tailnet-fqdn":"ledger-db.example-tailnet.ts.net"}`,
			`"type":"ExternalName","ports":[{"port":5432}]`, `"conditions":[{"type":"TailscaleProxyReady","status":"True"}]`)
		in.services = append(in.services, demoNetObject[netmap.Service](`{"metadata":{"name":"ledger-db","namespace":"payments"},
		  "spec":{"type":"ExternalName","ports":[{"port":5432}]}}`))
		proxy("ts-ledger-db-m2v8c-0", "demo-worker-2", "Running", 0, "svc", "ledger-db", "payments")
	case "Service/payments/legacy":
		in.object = demoTSService(`"name":"legacy","namespace":"payments","annotations":{"tailscale.com/tailnet-ip":"100.64.0.77"}`,
			`"type":"ExternalName","ports":[{"port":53,"protocol":"UDP"}]`,
			`"conditions":[{"type":"TailscaleProxyReady","status":"False","reason":"ProxyFailed","message":"error provisioning proxy: the tailnet refused the auth key"}]`)
		in.services = append(in.services, demoNetObject[netmap.Service](`{"metadata":{"name":"legacy","namespace":"payments"},
		  "spec":{"type":"ExternalName","ports":[{"port":53}]}}`))
	}

	return buildTailscaleNetwork(in)
}

// demoTSService is a Service's JSON from its metadata, spec and status fields.
func demoTSService(metadata, spec, status string) json.RawMessage {
	return json.RawMessage(`{"metadata":{` + metadata + `},"spec":{` + spec + `},"status":{` + status + `}}`)
}
