package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What explains a certificate's state, along its issuance chain: the certificate's conditions,
// its latest CertificateRequests, their ACME Orders and Challenges (whose reason says why a
// domain fails validation), the Kubernetes events of all of them and the lines of the
// cert-manager controller's log that name them.

const (
	groupACME = "acme.cert-manager.io"

	// certAnnotationName is the annotation cert-manager puts on a request: its certificate's name.
	certAnnotationName = "cert-manager.io/certificate-name"

	certMaxRequests  = 3    // the latest requests, the newest first
	certMaxEvents    = 40   // the newest events
	certMaxLogLines  = 60   // the newest log lines naming the chain
	certLogTailLines = 4000 // the controller log lines read to find them
)

// cert-manager's controller pods: the Helm chart's labels, then the older manifests' one.
var certControllerSelectors = []string{
	"app.kubernetes.io/name=cert-manager,app.kubernetes.io/component=controller",
	"app=cert-manager",
}

type certDetails struct {
	Conditions []certCondition     `json:"conditions"`
	Requests   []certRequestDetail `json:"requests"`
	Events     []certEvent         `json:"events"`
	// Log is the newest controller log lines naming the certificate or its chain, oldest first.
	Log   []string `json:"log"`
	Error string   `json:"error"` // what could not be read, the rest is still there
}

type certCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Time    int64  `json:"time"` // unix ms of the last transition, 0 when unknown
}

type certRequestDetail struct {
	Name       string            `json:"name"`
	Created    int64             `json:"created"` // unix ms
	Conditions []certCondition   `json:"conditions"`
	Orders     []acmeOrderDetail `json:"orders"`
}

type acmeOrderDetail struct {
	Name       string                `json:"name"`
	State      string                `json:"state"` // pending|ready|valid|invalid|errored…, "" before the first sync
	Reason     string                `json:"reason"`
	Challenges []acmeChallengeDetail `json:"challenges"`
}

type acmeChallengeDetail struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // HTTP-01|DNS-01
	DNSName   string `json:"dnsName"`
	Wildcard  bool   `json:"wildcard"`
	State     string `json:"state"`
	Reason    string `json:"reason"` // why it is not valid yet, e.g. the HTTP status a self check got
	Presented bool   `json:"presented"`
}

type certEvent struct {
	Time    int64  `json:"time"` // unix ms of the last occurrence
	Type    string `json:"type"` // Normal|Warning
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Object  string `json:"object"` // "CertificateRequest/site-1"
	Count   int    `json:"count"`
}

type cmObjectMeta struct {
	Name              string            `json:"name"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Annotations       map[string]string `json:"annotations"`
	OwnerReferences   []cmOwnerRef      `json:"ownerReferences"`
}

type cmOwnerRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type cmCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

type cmConditioned struct {
	Metadata cmObjectMeta `json:"metadata"`
	Status   struct {
		Conditions []cmCondition `json:"conditions"`
	} `json:"status"`
}

type acmeOrderObject struct {
	Metadata cmObjectMeta `json:"metadata"`
	Status   struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	} `json:"status"`
}

type acmeChallengeObject struct {
	Metadata cmObjectMeta `json:"metadata"`
	Spec     struct {
		Type     string `json:"type"`
		DNSName  string `json:"dnsName"`
		Wildcard bool   `json:"wildcard"`
	} `json:"spec"`
	Status struct {
		State     string `json:"state"`
		Reason    string `json:"reason"`
		Presented bool   `json:"presented"`
	} `json:"status"`
}

type kubeEventObject struct {
	Metadata struct {
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	InvolvedObject struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"involvedObject"`
	Type           string `json:"type"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Count          int    `json:"count"`
	FirstTimestamp string `json:"firstTimestamp"`
	LastTimestamp  string `json:"lastTimestamp"`
	EventTime      string `json:"eventTime"`
	Series         *struct {
		Count            int    `json:"count"`
		LastObservedTime string `json:"lastObservedTime"`
	} `json:"series"`
}

// KubeCertManagerDetails explains the state of the cert-manager certificate namespace/name
// (os:admin): its conditions, its latest requests with their ACME orders and challenges, their
// events and the controller log lines naming them. Parts that cannot be read are named in
// error, the others still come. kubeServer: see KubePods.
func KubeCertManagerDetails(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("certificate", namespace, name); err != nil {
		return "", err
	}

	demo := func() certDetails { return demoCertDetails(namespace, name, time.Now()) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (certDetails, error) {
		return readCertDetails(ctx, k, namespace, name)
	})
}

func readCertDetails(ctx context.Context, k *kubeClient, namespace, name string) (certDetails, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return certDetails{}, err
	}

	version, ok := groups[groupCertManager]
	if !ok {
		return certDetails{}, errCertManagerMissing
	}

	ns := "/namespaces/" + url.PathEscape(namespace) + "/"
	base := "/apis/" + groupCertManager + "/" + version + ns

	var (
		cert       cmConditioned
		requests   kubeList[cmConditioned]
		orders     kubeList[acmeOrderObject]
		challenges kubeList[acmeChallengeObject]
		events     kubeList[kubeEventObject]
		log        string
		errs       = make([]error, 6)
		wg         sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, base+"certificates/"+url.PathEscape(name), &cert) })
	wg.Go(func() { errs[1] = getList(ctx, k, base+"certificaterequests", &requests) })
	wg.Go(func() { errs[4] = getList(ctx, k, "/api/v1"+ns+"events", &events) })
	wg.Go(func() { log, errs[5] = readCertControllerLog(ctx, k) })

	// Orders and challenges exist only with ACME (the group is served with cert-manager).
	if acme, ok := groups[groupACME]; ok {
		acmeBase := "/apis/" + groupACME + "/" + acme + ns
		wg.Go(func() { errs[2] = getList(ctx, k, acmeBase+"orders", &orders) })
		wg.Go(func() { errs[3] = getList(ctx, k, acmeBase+"challenges", &challenges) })
	}

	wg.Wait()

	// Without the certificate there is nothing to explain.
	if errs[0] != nil {
		return certDetails{}, errs[0]
	}

	out := mapCertDetails(namespace, name, cert, requests.Items, orders.Items, challenges.Items, events.Items, log)
	out.Error = sectionError(errs[1:]...)

	return out, nil
}

func mapCertDetails(namespace, name string, cert cmConditioned, requests []cmConditioned, orders []acmeOrderObject,
	challenges []acmeChallengeObject, events []kubeEventObject, log string,
) certDetails {
	out := certDetails{Conditions: mapCMConditions(cert.Status.Conditions), Requests: []certRequestDetail{}, Events: []certEvent{}}

	// Every object of the chain, by kind, for the events and the log.
	chain := map[string][]string{"Certificate": {name}}

	mine := slices.DeleteFunc(slices.Clone(requests), func(r cmConditioned) bool {
		return r.Metadata.Annotations[certAnnotationName] != name && !ownedBy(r.Metadata, "Certificate", name)
	})
	slices.SortFunc(mine, func(a, b cmConditioned) int {
		return cmp.Compare(unixMilli(b.Metadata.CreationTimestamp), unixMilli(a.Metadata.CreationTimestamp))
	})

	for _, r := range mine[:min(len(mine), certMaxRequests)] {
		req := certRequestDetail{
			Name: r.Metadata.Name, Created: unixMilli(r.Metadata.CreationTimestamp),
			Conditions: mapCMConditions(r.Status.Conditions), Orders: []acmeOrderDetail{},
		}
		chain["CertificateRequest"] = append(chain["CertificateRequest"], r.Metadata.Name)

		for _, o := range orders {
			if !ownedBy(o.Metadata, "CertificateRequest", r.Metadata.Name) {
				continue
			}

			order := acmeOrderDetail{Name: o.Metadata.Name, State: o.Status.State, Reason: o.Status.Reason, Challenges: []acmeChallengeDetail{}}
			chain["Order"] = append(chain["Order"], o.Metadata.Name)

			for _, c := range challenges {
				if !ownedBy(c.Metadata, "Order", o.Metadata.Name) {
					continue
				}

				order.Challenges = append(order.Challenges, acmeChallengeDetail{
					Name: c.Metadata.Name, Type: c.Spec.Type, DNSName: c.Spec.DNSName, Wildcard: c.Spec.Wildcard,
					State: c.Status.State, Reason: c.Status.Reason, Presented: c.Status.Presented,
				})
				chain["Challenge"] = append(chain["Challenge"], c.Metadata.Name)
			}

			req.Orders = append(req.Orders, order)
		}

		out.Requests = append(out.Requests, req)
	}

	out.Events = mapCertEvents(events, chain)
	out.Log = certLogLines(log, namespace, chain)

	return out
}

func ownedBy(meta cmObjectMeta, kind, name string) bool {
	return slices.Contains(meta.OwnerReferences, cmOwnerRef{Kind: kind, Name: name})
}

func mapCMConditions(conds []cmCondition) []certCondition {
	out := make([]certCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, certCondition{Type: c.Type, Status: c.Status, Reason: c.Reason, Message: c.Message, Time: unixMilli(c.LastTransitionTime)})
	}

	return out
}

// mapCertEvents keeps the events of the chain's objects, the newest first.
func mapCertEvents(events []kubeEventObject, chain map[string][]string) []certEvent {
	out := []certEvent{}

	for _, e := range events {
		if !slices.Contains(chain[e.InvolvedObject.Kind], e.InvolvedObject.Name) {
			continue
		}

		ev := certEvent{
			Type: e.Type, Reason: e.Reason, Message: e.Message, Count: max(e.Count, 1),
			Object: e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
		}

		// events.k8s.io writers leave the core timestamps empty and count in a series.
		for _, t := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp, e.Metadata.CreationTimestamp} {
			if ev.Time = eventMilli(t); ev.Time != 0 {
				break
			}
		}

		if e.Series != nil {
			ev.Count = max(ev.Count, e.Series.Count)
			ev.Time = max(ev.Time, eventMilli(e.Series.LastObservedTime))
		}

		out = append(out, ev)
	}

	slices.SortStableFunc(out, func(a, b certEvent) int { return cmp.Compare(b.Time, a.Time) })

	return out[:min(len(out), certMaxEvents)]
}

// eventMilli parses an event time, RFC 3339 with or without microseconds.
func eventMilli(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}

	return t.UnixMilli()
}

// readCertControllerLog is the end of the log of cert-manager's controller pods; "" when none
// is found (cert-manager installed some other way).
func readCertControllerLog(ctx context.Context, k *kubeClient) (string, error) {
	for _, selector := range certControllerSelectors {
		var pods kubeList[dsPod]
		if err := getList(ctx, k, "/api/v1/pods?labelSelector="+url.QueryEscape(selector), &pods); err != nil {
			return "", err
		}

		var logs []string

		for _, p := range pods.Items {
			if p.Status.Phase != "Running" {
				continue
			}

			// Only the leader works: a standby replica's log just waits for the lease.
			log, err := k.getText(ctx, podPath(p.Metadata.Namespace, p.Metadata.Name)+"/log?tailLines="+strconv.Itoa(certLogTailLines))
			if err != nil {
				return "", err
			}

			logs = append(logs, log)
		}

		if len(logs) > 0 {
			return strings.Join(logs, "\n"), nil
		}
	}

	return "", nil
}

// certLogLines keeps the newest log lines naming an object of the chain and its namespace.
func certLogLines(log, namespace string, chain map[string][]string) []string {
	var names []string
	for _, n := range chain {
		names = append(names, n...)
	}

	out := []string{}

	for line := range strings.Lines(log) {
		line = strings.TrimRight(line, "\r\n")
		if strings.Contains(line, namespace) && slices.ContainsFunc(names, func(n string) bool { return mentionsName(line, n) }) {
			out = append(out, line)
		}
	}

	return out[max(0, len(out)-certMaxLogLines):]
}

// mentionsName: line holds name as a whole value, quoted or after a slash ("web/site",
// resource_name="site", "resource_name":"site"), so "site" does not match "site-2".
func mentionsName(line, name string) bool {
	for i := 0; ; {
		j := strings.Index(line[i:], name)
		if j < 0 {
			return false
		}

		start, end := i+j, i+j+len(name)
		before := start > 0 && strings.ContainsRune(`"/=`, rune(line[start-1]))
		after := end == len(line) || strings.ContainsRune(`" `, rune(line[end]))

		if before && after {
			return true
		}

		i = start + 1
	}
}
