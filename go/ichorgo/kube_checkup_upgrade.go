package ichorgo

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	// findDeprecatedAPI is an API version something still requests and a coming release
	// removes. Name: "resource.version.group", Extra: the release removing it ("1.32").
	findDeprecatedAPI = "deprecatedAPI"
	// An admission webhook whose Service has no ready endpoint. Name: the configuration,
	// Extra: "namespace/service", Reason: Validating or Mutating, Count: its webhooks there.
	findWebhookDown    = "webhookDown"    // failurePolicy Fail: the requests it matches are refused
	findWebhookSkipped = "webhookSkipped" // failurePolicy Ignore: it is silently not applied

	metricDeprecatedAPIs = "apiserver_requested_deprecated_apis"
	// webhookParallel bounds the Services looked up at once.
	webhookParallel = 4
)

// checkupUpgrade reads from the API server's /metrics which deprecated API versions were
// requested since it started. The metric does not say by whom: the audit analysis does.
func checkupUpgrade(ctx context.Context, k *kubeClient, version string) checkupSection {
	var samples promSamples

	err := k.stream(ctx, "/metrics", "text/plain, */*", func(body io.Reader) error {
		var err error
		samples, err = parsePromText(body, map[string]bool{metricDeprecatedAPIs: true})

		return err
	})
	if err != nil {
		return newSection(checkUpgrade, 0, nil, fmt.Errorf("read API server metrics: %w", err))
	}

	findings := deprecatedAPIFindings(samples, version)

	return newSection(checkUpgrade, len(findings), findings)
}

// deprecatedAPIFindings is critical for an API the next minor release removes (or an
// earlier one), a warning for a later release, a note when no removal is planned.
func deprecatedAPIFindings(samples promSamples, version string) []checkupFinding {
	major, minor, known := kubeMinor(version)
	requested := samples.sumBy(metricDeprecatedAPIs, nil, "group", "version", "resource", "removed_release")
	findings := []checkupFinding{}

	for _, key := range slices.Sorted(maps.Keys(requested)) {
		if requested[key] <= 0 {
			continue
		}

		parts := promSplit(key)
		group, apiVersion, resource, removed := parts[0], parts[1], parts[2], parts[3]
		f := checkupFinding{Kind: findDeprecatedAPI, Severity: sevWarning, Name: strings.TrimSuffix(resource+"."+apiVersion+"."+group, "."), Extra: removed}

		rMajor, rMinor, ok := kubeMinor(removed)

		switch {
		case !ok:
			f.Severity = sevInfo
		case known && (rMajor < major || (rMajor == major && rMinor <= minor+1)):
			f.Severity = sevCritical
		}

		findings = append(findings, f)
	}

	return findings
}

// kubeMinor reads the major and minor of a Kubernetes version ("v1.34.1", "1.32").
func kubeMinor(version string) (major, minor int, ok bool) {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}

	major, errMajor := strconv.Atoi(parts[0])
	// A minor may carry a suffix ("34+").
	minor, errMinor := strconv.Atoi(strings.TrimRight(parts[1], "+"))

	return major, minor, errMajor == nil && errMinor == nil
}

type webhookConfig struct {
	Metadata checkMeta `json:"metadata"`
	Webhooks []struct {
		Name          string `json:"name"`
		FailurePolicy string `json:"failurePolicy"`
		ClientConfig  struct {
			Service *struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"service"`
		} `json:"clientConfig"`
	} `json:"webhooks"`
}

type endpointSlice struct {
	Endpoints []struct {
		Conditions struct {
			Ready *bool `json:"ready"`
		} `json:"conditions"`
	} `json:"endpoints"`
}

// webhookTarget is the webhooks of one configuration that call one Service.
type webhookTarget struct {
	kind, config, namespace, service string
	fail                             bool
	count                            int
}

// checkupWebhooks finds the admission webhooks that call a Service nothing answers for.
func checkupWebhooks(ctx context.Context, k *kubeClient) checkupSection {
	const base = "/apis/admissionregistration.k8s.io/v1/"

	validating, vErr := listObjects[webhookConfig](ctx, k, base+"validatingwebhookconfigurations")
	mutating, mErr := listObjects[webhookConfig](ctx, k, base+"mutatingwebhookconfigurations")
	targets := append(webhookTargets("Validating", validating), webhookTargets("Mutating", mutating)...)

	services := []string{}

	for _, t := range targets {
		if key := t.namespace + "/" + t.service; !slices.Contains(services, key) {
			services = append(services, key)
		}
	}

	ready := make([]bool, len(services))
	errs := make([]error, len(services))

	forEachLimit(services, webhookParallel, func(i int, key string) {
		namespace, service, _ := strings.Cut(key, "/")
		ready[i], errs[i] = serviceHasReadyEndpoint(ctx, k, namespace, service)
	})

	down := map[string]bool{}

	for i, key := range services {
		down[key] = errs[i] == nil && !ready[i]
	}

	return newSection(checkWebhooks, len(validating)+len(mutating), webhookFindings(targets, down), append(errs, vErr, mErr)...)
}

// webhookTargets groups a list's webhooks by configuration and Service, in order. A webhook
// called by URL lives outside the cluster: nothing here can tell whether it answers.
func webhookTargets(kind string, configs []webhookConfig) []webhookTarget {
	var out []webhookTarget

	for _, c := range configs {
		first := len(out)

		for _, w := range c.Webhooks {
			svc := w.ClientConfig.Service
			if svc == nil {
				continue
			}

			i := slices.IndexFunc(out[first:], func(t webhookTarget) bool { return t.namespace == svc.Namespace && t.service == svc.Name })
			if i < 0 {
				out = append(out, webhookTarget{kind: kind, config: c.Metadata.Name, namespace: svc.Namespace, service: svc.Name})
				i = len(out) - 1 - first
			}

			out[first+i].count++
			// Fail is the default.
			out[first+i].fail = out[first+i].fail || w.FailurePolicy != "Ignore"
		}
	}

	return out
}

func webhookFindings(targets []webhookTarget, down map[string]bool) []checkupFinding {
	findings := []checkupFinding{}

	for _, t := range targets {
		if !down[t.namespace+"/"+t.service] {
			continue
		}

		f := checkupFinding{Kind: findWebhookSkipped, Severity: sevWarning, Name: t.config, Extra: t.namespace + "/" + t.service, Reason: t.kind, Count: t.count}
		if t.fail {
			f.Kind, f.Severity = findWebhookDown, sevCritical
		}

		findings = append(findings, f)
	}

	return findings
}

// serviceHasReadyEndpoint tells whether one endpoint of the Service is ready.
func serviceHasReadyEndpoint(ctx context.Context, k *kubeClient, namespace, service string) (bool, error) {
	path := "/apis/discovery.k8s.io/v1/namespaces/" + url.PathEscape(namespace) + "/endpointslices?labelSelector=" +
		url.QueryEscape("kubernetes.io/service-name="+service)

	list, err := listObjects[endpointSlice](ctx, k, path)
	if err != nil {
		return false, err
	}

	for _, s := range list {
		for _, e := range s.Endpoints {
			if e.Conditions.Ready == nil || *e.Conditions.Ready {
				return true, nil
			}
		}
	}

	return false, nil
}
