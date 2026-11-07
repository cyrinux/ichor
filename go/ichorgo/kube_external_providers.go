package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// The other providers' matchers (NetBird, Twingate, ngrok, Cloudflare Tunnel): each reads the
// provider's own objects, listed once per map build and only where the cluster serves the
// provider's API group, and names the Service they publish. They sit in externalMatchers next
// to the Tailscale one, so both maps draw them through the same box.

const (
	groupNetBird    = "netbird.io"
	groupTwingate   = "twingate.com"
	groupNgrok      = "ngrok.k8s.ngrok.com"
	groupCloudflare = "networking.cfargotunnel.com"
)

// externalList is one kind a matcher reads: its plural, at the group's preferred version unless
// version says otherwise (the legacy NetBird kinds).
type externalList struct{ plural, version string }

// externalObjects are the objects the matchers' lists brought back, by plural.
type externalObjects map[string][]json.RawMessage

// readExternalObjects lists, in parallel, the kinds of every matcher whose group groups serves
// (none: no request). A kind the server does not have is empty; any other failure is returned
// with what was read.
func readExternalObjects(ctx context.Context, k *kubeClient, groups map[string]string) (externalObjects, error) {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs []error
		out  = externalObjects{}
	)

	for _, m := range externalMatchers {
		preferred, served := groups[m.group]
		if !served {
			continue
		}

		for _, l := range m.lists {
			version := preferred
			if l.version != "" {
				version = l.version
			}

			wg.Go(func() {
				items, err := listObjects[json.RawMessage](ctx, k, "/apis/"+m.group+"/"+version+"/"+l.plural)
				err = ignoreNotFound(err)

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					errs = append(errs, err)
				}

				if len(items) > 0 {
					out[l.plural] = items
				}
			})
		}
	}

	wg.Wait()

	if len(errs) > 0 {
		return out, errs[0]
	}

	return out, nil
}

// extObject is the part of any provider object the matchers read.
type extObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Conditions []extCondition `json:"conditions"`
	} `json:"status"`
}

type extCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (c extCondition) problem() string { return strings.Trim(c.Reason+": "+c.Message, ": ") }

// extObjectsOf decodes each raw of list into T, skipping what does not decode.
func extObjectsOf[T any](raws []json.RawMessage) []T {
	var out []T

	for _, raw := range raws {
		var v T
		if json.Unmarshal(raw, &v) == nil {
			out = append(out, v)
		}
	}

	return out
}

// extServiceOf is the namespace and name of the Service raw; false for anything else (the
// provider matchers publish Services only).
func extServiceOf(kind string, raw json.RawMessage) (namespace, name string, ok bool) {
	if kind != tsKindService {
		return "", "", false
	}

	var o extObject
	if json.Unmarshal(raw, &o) != nil || o.Metadata.Name == "" {
		return "", "", false
	}

	return o.Metadata.Namespace, o.Metadata.Name, true
}

// clusterHostService is the Service a cluster host names: "svc", "svc.ns", "svc.ns.svc" or
// "svc.ns.svc.cluster.local" (a bare name in defaultNS, "" when the host must carry its
// namespace). Any other host is not a Service.
func clusterHostService(host, defaultNS string) (namespace, name string, ok bool) {
	host = strings.TrimSuffix(strings.TrimSuffix(host, "."), ".cluster.local")
	labels := strings.Split(host, ".")

	switch {
	case len(labels) == 1 && labels[0] != "" && defaultNS != "":
		return defaultNS, labels[0], true
	case len(labels) == 2 && labels[0] != "" && labels[1] != "":
		return labels[1], labels[0], true
	case len(labels) == 3 && labels[2] == "svc" && labels[0] != "" && labels[1] != "":
		return labels[1], labels[0], true
	}

	return "", "", false
}

// extEntry is an entry of provider with its health and detail: the provider first, then the
// problem when there is one.
func extEntry(provider, name, health, problem string) externalEntry {
	e := externalEntry{Provider: provider, Kind: externalIngress, Name: name, Health: health, Addresses: []string{name}, Detail: provider}
	if problem != "" {
		e.Detail += " · " + problem
	}

	return e
}

// extFalse is the first condition of conds of one of types that is False.
func extFalse(conds []extCondition, types ...string) (extCondition, bool) {
	for _, c := range conds {
		if c.Status == "False" && (len(types) == 0 || slices.Contains(types, c.Type)) {
			return c, true
		}
	}

	return extCondition{}, false
}

// extWebURL is name as a link when it is a web address.
func extWebURL(name string) string {
	if strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "http://") {
		return name
	}

	return ""
}

// NetBird.

const nbProvider = "NetBird"

type nbResource struct {
	extObject
	Spec struct {
		ServiceRef       struct{ Name string } `json:"serviceRef"`
		NetworkRouterRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"networkRouterRef"`
		Address string `json:"address"` // the legacy NBResource
	} `json:"spec"`
	Status struct {
		ResourceID        string         `json:"resourceID"`
		NetworkResourceID string         `json:"networkResourceID"` // the legacy NBResource's
		Conditions        []extCondition `json:"conditions"`
	} `json:"status"`
}

type nbRouter struct {
	extObject
	Spec struct {
		DNSZoneRef struct{ Name string } `json:"dnsZoneRef"`
	} `json:"spec"`
}

// nbExternalEntries: a NetworkResource whose serviceRef is the Service, by the DNS record the
// operator creates for it ("<service>.<namespace>.<zone>", the zone of the router it
// references) when the router is known, else by the resource's namespace/name; a legacy
// NBResource whose address is the Service.
func nbExternalEntries(kind string, raw json.RawMessage, objs externalObjects) []externalEntry {
	ns, name, ok := extServiceOf(kind, raw)
	if !ok {
		return nil
	}

	var out []externalEntry

	routers := extObjectsOf[nbRouter](objs["networkrouters"])

	for _, r := range extObjectsOf[nbResource](objs["networkresources"]) {
		if r.Metadata.Namespace != ns || r.Spec.ServiceRef.Name != name {
			continue
		}

		entry := r.Metadata.Namespace + "/" + r.Metadata.Name

		routerNS := cmp.Or(r.Spec.NetworkRouterRef.Namespace, r.Metadata.Namespace)
		for _, router := range routers {
			if router.Metadata.Namespace == routerNS && router.Metadata.Name == r.Spec.NetworkRouterRef.Name && router.Spec.DNSZoneRef.Name != "" {
				entry = name + "." + ns + "." + strings.TrimSuffix(router.Spec.DNSZoneRef.Name, ".")
			}
		}

		out = append(out, nbEntry(entry, r))
	}

	for _, r := range extObjectsOf[nbResource](objs["nbresources"]) {
		if rns, rname, ok := clusterHostService(r.Spec.Address, ""); ok && rns == ns && rname == name {
			out = append(out, nbEntry(r.Metadata.Name, r))
		}
	}

	return out
}

func nbEntry(name string, r nbResource) externalEntry {
	if c, bad := extFalse(r.Status.Conditions); bad {
		return extEntry(nbProvider, name, healthCritical, c.problem())
	}

	if r.Status.ResourceID == "" && r.Status.NetworkResourceID == "" {
		return extEntry(nbProvider, name, healthWarning, "not published yet")
	}

	return extEntry(nbProvider, name, healthOK, "")
}

// Twingate.

const tgProvider = "Twingate"

type tgResource struct {
	extObject
	Spec struct {
		ID      string `json:"id"`
		Address string `json:"address"`
		Alias   string `json:"alias"`
	} `json:"spec"`
}

// tgExternalEntries: a TwingateResource whose address is the Service ("svc.ns", "svc.ns.svc" or
// the full name), by its alias when it has one. spec.id is set once Twingate has created it.
func tgExternalEntries(kind string, raw json.RawMessage, objs externalObjects) []externalEntry {
	ns, name, ok := extServiceOf(kind, raw)
	if !ok {
		return nil
	}

	var out []externalEntry

	for _, r := range extObjectsOf[tgResource](objs["twingateresources"]) {
		if rns, rname, ok := clusterHostService(r.Spec.Address, ""); !ok || rns != ns || rname != name {
			continue
		}

		entry := r.Spec.Address
		if r.Spec.Alias != "" {
			entry = r.Spec.Alias
		}

		if r.Spec.ID == "" {
			out = append(out, extEntry(tgProvider, entry, healthWarning, "not created yet"))
		} else {
			out = append(out, extEntry(tgProvider, entry, healthOK, ""))
		}
	}

	return out
}

// ngrok.

const ngProvider = "ngrok"

type ngAgentEndpoint struct {
	extObject
	Spec struct {
		URL      string `json:"url"`
		Upstream struct {
			URL string `json:"url"`
		} `json:"upstream"`
	} `json:"spec"`
	Status struct {
		AssignedURL string         `json:"assignedURL"`
		Conditions  []extCondition `json:"conditions"`
	} `json:"status"`
}

// ngExternalEntries: an AgentEndpoint whose upstream host is the Service (its first label, in the
// endpoint's namespace unless the host names another), by its assigned URL, else its own.
func ngExternalEntries(kind string, raw json.RawMessage, objs externalObjects) []externalEntry {
	ns, name, ok := extServiceOf(kind, raw)
	if !ok {
		return nil
	}

	var out []externalEntry

	for _, e := range extObjectsOf[ngAgentEndpoint](objs["agentendpoints"]) {
		up, err := url.Parse(e.Spec.Upstream.URL)
		if err != nil {
			continue
		}

		if uns, uname, ok := clusterHostService(up.Hostname(), e.Metadata.Namespace); !ok || uns != ns || uname != name {
			continue
		}

		entry := e.Spec.URL
		if e.Status.AssignedURL != "" {
			entry = e.Status.AssignedURL
		}

		x := ngEntry(entry, e)
		x.URL = extWebURL(entry)
		out = append(out, x)
	}

	return out
}

func ngEntry(name string, e ngAgentEndpoint) externalEntry {
	if c, bad := extFalse(e.Status.Conditions, "EndpointCreated", "DomainReady"); bad {
		return extEntry(ngProvider, name, healthCritical, c.problem())
	}

	if c, bad := extFalse(e.Status.Conditions, "Ready"); bad {
		return extEntry(ngProvider, name, healthWarning, c.problem())
	}

	return extEntry(ngProvider, name, healthOK, "")
}

// Cloudflare Tunnel (the community operator).

const cfProvider = "Cloudflare"

type cfBinding struct {
	extObject
	Subjects []struct {
		Name string `json:"name"`
		Spec struct {
			FQDN string `json:"fqdn"`
		} `json:"spec"`
	} `json:"subjects"`
	Status struct {
		// Services are the published hostnames, one per subject, in the subjects' order.
		Services []struct {
			Hostname string `json:"hostname"`
		} `json:"services"`
	} `json:"status"`
}

// cfExternalEntries: a TunnelBinding of the Service's namespace with a subject naming it, by the
// hostname the operator published for that subject (status.services, same index); before that,
// a warning named by the subject's fqdn, else the binding's namespace/name.
func cfExternalEntries(kind string, raw json.RawMessage, objs externalObjects) []externalEntry {
	ns, name, ok := extServiceOf(kind, raw)
	if !ok {
		return nil
	}

	var out []externalEntry

	for _, b := range extObjectsOf[cfBinding](objs["tunnelbindings"]) {
		if b.Metadata.Namespace != ns {
			continue
		}

		for i, s := range b.Subjects {
			if s.Name != name {
				continue
			}

			if i < len(b.Status.Services) && b.Status.Services[i].Hostname != "" {
				host := b.Status.Services[i].Hostname
				e := extEntry(cfProvider, host, healthOK, "")
				e.URL = "https://" + host
				out = append(out, e)

				continue
			}

			out = append(out, extEntry(cfProvider, cmp.Or(s.Spec.FQDN, b.Metadata.Namespace+"/"+b.Metadata.Name), healthWarning, "not published yet"))
		}
	}

	return out
}
