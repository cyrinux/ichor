package ichorgo

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// What Prometheus fails to scrape (CYR-35): its active targets, grouped by scrape pool,
// only the down ones listed. The Prometheus Operator names a pool after the monitor that
// made it, which leads back to the ServiceMonitor or PodMonitor to fix.

// promMaxDownTargets bounds the down targets listed; the counts still cover them all.
const promMaxDownTargets = 500

const (
	promHealthUp   = "up"
	promHealthDown = "down"
)

// The scrape pool prefixes the Prometheus Operator writes, and the kinds they stand for.
var promPoolKinds = map[string]string{
	"serviceMonitor": "ServiceMonitor",
	"podMonitor":     "PodMonitor",
	"probe":          "Probe",
	"scrapeConfig":   "ScrapeConfig",
}

// promTargetsResult is the app's view of GET /api/v1/targets?state=active.
type promTargetsResult struct {
	Pools     []promTargetPool `json:"pools"`
	Up        int              `json:"up"`
	Down      int              `json:"down"`
	Unknown   int              `json:"unknown"` // not scraped yet
	Total     int              `json:"total"`
	Truncated bool             `json:"truncated"`
}

// promTargetPool is one scrape pool. Kind, Namespace and Name are the monitor that made
// it (Kind "" for a job of the configuration itself), Endpoint its endpoint index (-1 for
// none).
type promTargetPool struct {
	Pool      string       `json:"pool"`
	Kind      string       `json:"kind"`
	Namespace string       `json:"namespace"`
	Name      string       `json:"name"`
	Endpoint  int          `json:"endpoint"`
	Up        int          `json:"up"`
	Down      int          `json:"down"`
	Unknown   int          `json:"unknown"`
	Targets   []promTarget `json:"targets"` // the down ones
}

// promTarget is a down target, with the labels that lead to its Service and pod.
type promTarget struct {
	ScrapeURL          string  `json:"scrapeUrl"`
	LastError          string  `json:"lastError"`
	LastScrape         int64   `json:"lastScrape"`         // unix ms, 0 before the first
	LastScrapeDuration float64 `json:"lastScrapeDuration"` // seconds
	Job                string  `json:"job"`
	Namespace          string  `json:"namespace"`
	Service            string  `json:"service"`
	Pod                string  `json:"pod"`
	Instance           string  `json:"instance"`
}

type promRawTarget struct {
	Labels             map[string]string `json:"labels"`
	ScrapePool         string            `json:"scrapePool"`
	ScrapeURL          string            `json:"scrapeUrl"`
	LastError          string            `json:"lastError"`
	LastScrape         string            `json:"lastScrape"`
	LastScrapeDuration float64           `json:"lastScrapeDuration"`
	Health             string            `json:"health"`
}

// PromTargets lists what the metrics source sourceJSON scrapes (GET
// /api/v1/targets?state=active), as JSON {pools:[{pool,kind:"ServiceMonitor"|"PodMonitor"|
// "Probe"|"ScrapeConfig"|"",namespace,name,endpoint,up,down,unknown,targets:[{scrapeUrl,
// lastError,lastScrape (unix ms),lastScrapeDuration (s),job,namespace,service,pod,
// instance}]}],up,down,unknown,total,truncated}: only the down targets are listed, the
// pools with the most first. kubeServer is as for PromQueryRange.
func PromTargets(configYAML, contextName, kubeServer, sourceJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoPromTargets(time.Now()))
	}

	target := kubeTarget{configYAML, contextName, kubeServer}

	data, err := promAPIData(src, promAPIPathTargets)(promSend(target, src, http.MethodGet, promAPIPathTargets, nil))
	if err != nil {
		return "", err
	}

	res, err := parsePromTargets(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src.label(), err)
	}

	return toJSON(res)
}

// parsePromTargets reads the data of a targets answer.
func parsePromTargets(data json.RawMessage) (promTargetsResult, error) {
	var raw struct {
		ActiveTargets []promRawTarget `json:"activeTargets"`
	}

	if err := json.Unmarshal(data, &raw); err != nil || raw.ActiveTargets == nil {
		return promTargetsResult{}, errors.New("the targets answer has no active targets")
	}

	return groupPromTargets(raw.ActiveTargets), nil
}

func groupPromTargets(targets []promRawTarget) promTargetsResult {
	res := promTargetsResult{Pools: []promTargetPool{}, Total: len(targets)}
	index := map[string]int{}
	listed := 0

	for _, t := range targets {
		i, ok := index[t.ScrapePool]
		if !ok {
			i = len(res.Pools)
			index[t.ScrapePool] = i
			res.Pools = append(res.Pools, parseScrapePool(t.ScrapePool))
		}

		p := &res.Pools[i]

		switch strings.ToLower(t.Health) {
		case promHealthUp:
			p.Up++
			res.Up++
		case promHealthDown:
			p.Down++
			res.Down++

			if listed == promMaxDownTargets {
				res.Truncated = true

				continue
			}

			listed++

			p.Targets = append(p.Targets, promTarget{
				ScrapeURL: t.ScrapeURL, LastError: clipUTF8(strings.TrimSpace(t.LastError), promMaxRuleError),
				LastScrape: promTime(t.LastScrape), LastScrapeDuration: t.LastScrapeDuration,
				Job: t.Labels["job"], Namespace: t.Labels["namespace"], Service: t.Labels["service"], Pod: t.Labels["pod"], Instance: t.Labels["instance"],
			})
		default:
			p.Unknown++
			res.Unknown++
		}
	}

	for i := range res.Pools {
		slices.SortFunc(res.Pools[i].Targets, func(a, b promTarget) int {
			return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Pod, b.Pod), cmp.Compare(a.Instance, b.Instance))
		})
	}

	slices.SortFunc(res.Pools, func(a, b promTargetPool) int {
		return cmp.Or(b.Down-a.Down, cmp.Compare(a.Pool, b.Pool))
	})

	return res
}

// parseScrapePool reads the monitor out of a pool the operator named
// serviceMonitor/<namespace>/<name>/<endpoint>, podMonitor/… likewise, probe/<namespace>/
// <name> or scrapeConfig/<namespace>/<name>; any other pool is a job of the configuration.
func parseScrapePool(pool string) promTargetPool {
	p := promTargetPool{Pool: pool, Endpoint: -1, Targets: []promTarget{}}
	parts := strings.Split(pool, "/")

	kind, ok := promPoolKinds[parts[0]]
	if !ok || len(parts) < 3 || len(parts) > 4 || parts[1] == "" || parts[2] == "" {
		return p
	}

	p.Kind, p.Namespace, p.Name = kind, parts[1], parts[2]

	if len(parts) == 4 {
		if n, err := strconv.Atoi(parts[3]); err == nil && n >= 0 {
			p.Endpoint = n
		}
	}

	return p
}
