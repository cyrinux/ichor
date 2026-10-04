package ichorgo

import (
	"encoding/json"
	"time"
)

// The demo's policies, in their Kubernetes form so the real mapping runs on them.
const (
	demoNetworkPoliciesJSON = `{"items":[
 {"metadata":{"name":"immich-postgres-ingress","namespace":"media"},
  "spec":{"podSelector":{"matchLabels":{"app":"immich-postgres"}},"policyTypes":["Ingress"],
   "ingress":[{"from":[{"podSelector":{"matchLabels":{"app":"immich-server"}}},{"podSelector":{"matchLabels":{"app":"immich-machine-learning"}}}],
               "ports":[{"protocol":"TCP","port":5432}]}]}},
 {"metadata":{"name":"default-deny-egress","namespace":"home"},"spec":{"podSelector":{},"policyTypes":["Egress"]}},
 {"metadata":{"name":"allow-prometheus-scrape","namespace":"media"},
  "spec":{"podSelector":{},"ingress":[{"from":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"monitoring"}},
   "podSelector":{"matchLabels":{"app":"prometheus-kube-prometheus"}}}],"ports":[{"port":"metrics"}]}]}}
]}`
	demoCiliumPoliciesJSON = `{"items":[
 {"metadata":{"name":"mosquitto","namespace":"home"},
  "spec":{"description":"MQTT only from the home automation apps","endpointSelector":{"matchLabels":{"app":"mosquitto"}},
   "ingress":[{"fromEndpoints":[{"matchLabels":{"app":"zigbee2mqtt"}},{"matchLabels":{"app":"home-assistant"}}],
               "toPorts":[{"ports":[{"port":"1883","protocol":"TCP"}]}]}]}},
 {"metadata":{"name":"home-egress","namespace":"home"},
  "spec":{"endpointSelector":{"matchLabels":{"app":"home-assistant"}},
   "egress":[{"toEndpoints":[{"matchLabels":{"app":"mosquitto"}}],"toPorts":[{"ports":[{"port":"1883","protocol":"TCP"}]}]},
             {"toFQDNs":[{"matchPattern":"*.home-assistant.io"}],"toPorts":[{"ports":[{"port":"443","protocol":"TCP"}]}]}],
   "egressDeny":[{"toEntities":["world"],"toPorts":[{"ports":[{"port":"25","protocol":"TCP"}]}]}]}},
 {"metadata":{"name":"vaultwarden-web","namespace":"default"},
  "spec":{"endpointSelector":{"matchLabels":{"app":"vaultwarden"}},
   "ingress":[{"fromEndpoints":[{"matchLabels":{"k8s:io.kubernetes.pod.namespace":"networking","app":"traefik"}}],
               "toPorts":[{"ports":[{"port":"8080","protocol":"TCP"}],"rules":{"http":[{"method":"GET"},{"method":"POST","path":"/api/.*"}]}}]}]}}
]}`
	demoClusterPoliciesJSON = `{"items":[
 {"metadata":{"name":"allow-dns"},
  "spec":{"description":"Every pod may resolve names","endpointSelector":{},"enableDefaultDeny":{"egress":false,"ingress":false},
   "egress":[{"toEndpoints":[{"matchLabels":{"k8s:io.kubernetes.pod.namespace":"kube-system","k8s:k8s-app":"kube-dns"}}],
              "toPorts":[{"ports":[{"port":"53","protocol":"ANY"}],"rules":{"dns":[{"matchPattern":"*"}]}}]}]}},
 {"metadata":{"name":"control-plane-host"},
  "spec":{"nodeSelector":{"matchLabels":{"node-role.kubernetes.io/control-plane":""}},
   "ingress":[{"fromEntities":["cluster"]},{"fromEntities":["world"],"toPorts":[{"ports":[{"port":"6443","protocol":"TCP"},{"port":"50000","protocol":"TCP"}]}]}]}}
]}`
)

func demoNetPolicies() netPolicyReport {
	var (
		nps         kubeList[npObject]
		cnps, ccnps kubeList[cnpObject]
		created     = time.Now().Add(-21 * 24 * time.Hour).UnixMilli()
		policies    []netPolicy
		pods        []npPod
		namespaces  []npNamespace
		seenNS      = map[string]bool{}
	)

	_ = json.Unmarshal([]byte(demoNetworkPoliciesJSON), &nps)
	_ = json.Unmarshal([]byte(demoCiliumPoliciesJSON), &cnps)
	_ = json.Unmarshal([]byte(demoClusterPoliciesJSON), &ccnps)

	for _, o := range nps.Items {
		policies = append(policies, mapNetworkPolicy(o))
	}

	for _, o := range cnps.Items {
		policies = append(policies, mapCiliumPolicy(o, false))
	}

	for _, o := range ccnps.Items {
		policies = append(policies, mapCiliumPolicy(o, true))
	}

	for i := range policies {
		policies[i].Created = created + int64(i)*int64(time.Hour/time.Millisecond)
	}

	for _, w := range demoWorkloads {
		var pod npPod
		pod.Metadata.Name, pod.Metadata.Namespace = w.pod, w.namespace
		pod.Metadata.Labels = map[string]string{"app": podBaseName(w.pod)}
		pod.Status.Phase = "Running"

		if w.namespace == "kube-system" && podBaseName(w.pod) == "coredns" {
			pod.Metadata.Labels["k8s-app"] = "kube-dns"
		}

		// The Cilium agents and control plane pods run on the host network.
		pod.Spec.HostNetwork = w.namespace == "kube-system" && podBaseName(w.pod) != "coredns" && podBaseName(w.pod) != "metrics-server"
		pods = append(pods, pod)

		if !seenNS[w.namespace] {
			seenNS[w.namespace] = true

			var ns npNamespace
			ns.Metadata.Name = w.namespace
			ns.Metadata.Labels = map[string]string{"kubernetes.io/metadata.name": w.namespace}
			namespaces = append(namespaces, ns)
		}
	}

	report := netPolicyReport{Cilium: true, Policies: policies}
	report.Namespaces = attachPolicyPods(report.Policies, pods, namespaces)

	return report
}
