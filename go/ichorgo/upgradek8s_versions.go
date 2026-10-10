package ichorgo

import (
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The versions the Kubernetes upgrade screens offer: the latest patch of the minor the cluster
// runs and of the next one, within the range its Talos supports. See K8sUpgradeVersions.

// k8sStableURL serves stable-1.N.txt, the latest patch of each minor (tests replace it).
var k8sStableURL = "https://dl.k8s.io/release"

const k8sStableTimeout = 10 * time.Second

// k8sVersionChoice is what K8sUpgradeVersions returns.
type k8sVersionChoice struct {
	From string `json:"from"` // the API server's version now, "1.34.0"; "" when unknown
	// SupportedRange is "1.30–1.35" (what every node's Talos supports), "" when unknown; Lo
	// and Hi are its minors (0 when unknown).
	SupportedRange string `json:"supportedRange"`
	Lo             int    `json:"lo"`
	Hi             int    `json:"hi"`
	// Versions are the upgrades offered, oldest first: newer than From, at most one minor up,
	// inside the range. Empty when none or when the release list could not be read.
	Versions []string `json:"versions"`
	// Warning says why Versions may be missing some (the release list could not be read).
	Warning string `json:"warning,omitempty"`
}

// K8sUpgradeVersions says which Kubernetes versions the cluster can be upgraded to (os:admin,
// it reads the machine configs like K8sUpgradePlan): the version it runs, the range its Talos
// supports, and the latest patch release of the current minor and of the next one inside that
// range, from dl.k8s.io. A version from the list still goes through K8sUpgradePlan's checks.
func K8sUpgradeVersions(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isKubeconfig(configYAML) {
		return "", errK8sUpgradeNoTalos
	}

	// The demo reads no network: its offer is made from its own version.
	if isDemoContext(configYAML, contextName) {
		text, err := demoRead("K8sUpgradePlan", configYAML, contextName, "", "1.0.0")
		if err != nil {
			return "", err
		}

		var plan k8sUpgradePlan
		if err := stdjson.Unmarshal([]byte(text), &plan); err != nil {
			return "", err
		}

		return toJSON(k8sChoices(context.Background(), plan, demoK8sStable))
	}

	return withSession(configYAML, contextName, clusterPlanTimeout(configYAML, contextName), func(ctx context.Context, s *session) (string, error) {
		// Any valid version gives the current one and the range; its own checks are not used.
		plan, err := buildK8sPlan(readK8sNodes(ctx, s), "1.0.0")
		if err != nil {
			return "", err
		}

		return toJSON(k8sChoices(ctx, plan, fetchK8sStable))
	})
}

// k8sChoices offers the latest patch of From's minor and of the next one, inside the range;
// stable gives the latest patch of a minor ("1.34.3").
func k8sChoices(ctx context.Context, plan k8sUpgradePlan, stable func(context.Context, int) (string, error)) k8sVersionChoice {
	choice := k8sVersionChoice{From: plan.From, SupportedRange: plan.SupportedRange, Versions: []string{}}
	choice.Lo, choice.Hi = parseK8sRange(plan.SupportedRange)

	_, minor, ok := kubeMinor(plan.From)
	if !ok {
		return choice
	}

	top := minor + 1
	if choice.Hi > 0 {
		top = min(top, choice.Hi)
	}

	for m := minor; m <= top; m++ {
		if choice.Lo > 0 && m < choice.Lo {
			continue
		}

		v, err := stable(ctx, m)
		if err != nil {
			choice.Warning = "the latest Kubernetes releases could not be read (" + err.Error() + "): type a version"

			continue
		}

		if newerK8s(v, plan.From) {
			choice.Versions = append(choice.Versions, v)
		}
	}

	return choice
}

// parseK8sRange reads "1.30–1.35" into its minors (0, 0 when unknown).
func parseK8sRange(text string) (lo, hi int) {
	parts := strings.Split(text, "–")
	if len(parts) != 2 {
		return 0, 0
	}

	_, lo, okLo := kubeMinor(parts[0])
	_, hi, okHi := kubeMinor(parts[1])

	if !okLo || !okHi {
		return 0, 0
	}

	return lo, hi
}

// newerK8s: version a (1.X.Y) is after b.
func newerK8s(a, b string) bool {
	pa, pb := k8sParts(a), k8sParts(b)

	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}

	return false
}

func k8sParts(v string) [3]int {
	var out [3]int

	for i, p := range strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3) {
		n, _ := strconv.Atoi(strings.TrimRight(p, "+")) //nolint:errcheck // a part that is not a number counts 0
		out[i] = n
	}

	return out
}

// fetchK8sStable reads the latest patch of 1.minor from dl.k8s.io.
func fetchK8sStable(ctx context.Context, minor int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, k8sStableTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/stable-1.%d.txt", k8sStableURL, minor), nil)
	if err != nil {
		return "", err
	}

	resp, err := newHTTPClient(httpClientOpts{followRedirects: true}).Do(req)
	if err != nil {
		return "", errors.New(friendlyError(err))
	}

	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dl.k8s.io answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}

	v := strings.TrimPrefix(strings.TrimSpace(string(body)), "v")
	if p := k8sParts(v); p[0] != 1 || p[1] != minor {
		return "", fmt.Errorf("unexpected release %q for 1.%d", v, minor)
	}

	return v, nil
}

// demoK8sStable: a patch release after the demo's version, and the next minor's first.
func demoK8sStable(_ context.Context, minor int) (string, error) {
	_, current, _ := kubeMinor(demoK8sVersion)
	if minor == current {
		p := k8sParts(demoK8sVersion)

		return fmt.Sprintf("1.%d.%d", minor, p[2]+2), nil
	}

	return fmt.Sprintf("1.%d.1", minor), nil
}
