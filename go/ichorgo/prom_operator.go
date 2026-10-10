package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
)

// The Prometheus Operator (CYR-35), read through the Kubernetes API: its Prometheus and
// Alertmanager servers, ready or not, and how many monitors and rules it works from.

const (
	groupMonitoring  = "monitoring.coreos.com"
	promOperatorAPI  = "/apis/" + groupMonitoring + "/v1"
	promOperatorPath = promOperatorAPI + "/"
)

// promOperatorStatus is the operator's state; Installed is false (and the rest empty)
// without its CRDs. Error says which lists could not be read.
type promOperatorStatus struct {
	Installed       bool                 `json:"installed"`
	Error           string               `json:"error"`
	Prometheuses    []promOperatorServer `json:"prometheuses"`
	Alertmanagers   []promOperatorServer `json:"alertmanagers"`
	ServiceMonitors int                  `json:"serviceMonitors"`
	PodMonitors     int                  `json:"podMonitors"`
	PrometheusRules int                  `json:"prometheusRules"`
	Probes          int                  `json:"probes"`
}

// promOperatorServer is a Prometheus or an Alertmanager object. Desired is Replicas ×
// Shards (Shards is 1 for an Alertmanager), Available what its status counts. Health is
// critical (none available, or Available False), warning (fewer than desired, degraded or
// not reconciled) or ok.
type promOperatorServer struct {
	Namespace  string                  `json:"namespace"`
	Name       string                  `json:"name"`
	Version    string                  `json:"version"`
	Replicas   int                     `json:"replicas"`
	Shards     int                     `json:"shards"`
	Desired    int                     `json:"desired"`
	Available  int                     `json:"available"`
	Paused     bool                    `json:"paused"`
	Health     string                  `json:"health"`
	Conditions []promOperatorCondition `json:"conditions"` // Available and Reconciled, as set
}

type promOperatorCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"` // True, False, Degraded (Available), Unknown
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type promOperatorObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Replicas *int   `json:"replicas"`
		Shards   *int   `json:"shards"`
		Version  string `json:"version"`
		Paused   bool   `json:"paused"`
	} `json:"spec"`
	Status struct {
		AvailableReplicas int             `json:"availableReplicas"`
		Conditions        []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// PromOperatorStatus reads the Prometheus Operator's objects (monitoring.coreos.com/v1),
// as JSON {installed,error,prometheuses:[{namespace,name,version,replicas,shards,desired,
// available,paused,health:"critical"|"warning"|"ok",conditions:[{type:"Available"|
// "Reconciled",status,reason,message}]}],alertmanagers:[same],serviceMonitors,podMonitors,
// prometheusRules,probes}, the servers in trouble first; installed is false without the
// operator's CRDs. kubeServer: see KubePods.
func PromOperatorStatus(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoPromOperator, readPromOperator)
}

func readPromOperator(ctx context.Context, k *kubeClient) (promOperatorStatus, error) {
	var discovery struct {
		Resources []struct {
			Name string `json:"name"`
		} `json:"resources"`
	}

	out := promOperatorStatus{Prometheuses: []promOperatorServer{}, Alertmanagers: []promOperatorServer{}}

	if err := k.get(ctx, promOperatorAPI, &discovery); isNotFound(err) {
		return out, nil
	} else if err != nil {
		return out, err
	}

	served := map[string]bool{}
	for _, r := range discovery.Resources {
		served[r.Name] = true
	}

	out.Installed = true

	var (
		wg   sync.WaitGroup
		errs = make([]error, 6)
	)

	servers := func(resource string, shards bool, into *[]promOperatorServer, err *error) {
		if served[resource] {
			wg.Go(func() { *into, *err = listPromServers(ctx, k, resource, shards) })
		}
	}

	count := func(resource string, into *int, err *error) {
		if served[resource] {
			wg.Go(func() { *into, *err = countObjects(ctx, k, promOperatorPath+resource) })
		}
	}

	servers("prometheuses", true, &out.Prometheuses, &errs[0])
	servers("alertmanagers", false, &out.Alertmanagers, &errs[1])
	count("servicemonitors", &out.ServiceMonitors, &errs[2])
	count("podmonitors", &out.PodMonitors, &errs[3])
	count("prometheusrules", &out.PrometheusRules, &errs[4])
	count("probes", &out.Probes, &errs[5])
	wg.Wait()

	out.Error = sectionError(errs...)
	out.Prometheuses, out.Alertmanagers = nonNil(out.Prometheuses), nonNil(out.Alertmanagers)

	return out, nil
}

// listPromServers lists the Prometheus (shards: true) or Alertmanager objects.
func listPromServers(ctx context.Context, k *kubeClient, resource string, shards bool) ([]promOperatorServer, error) {
	objs, err := listObjects[promOperatorObject](ctx, k, promOperatorPath+resource)
	if err != nil {
		return nil, err
	}

	out := make([]promOperatorServer, 0, len(objs))
	for _, o := range objs {
		out = append(out, promServerOf(o, shards))
	}

	slices.SortFunc(out, byHealthThenKey(func(s promOperatorServer) (string, string) { return s.Health, s.Namespace + "/" + s.Name }))

	return out, nil
}

func promServerOf(o promOperatorObject, shards bool) promOperatorServer {
	s := promOperatorServer{
		Namespace: o.Metadata.Namespace, Name: o.Metadata.Name, Version: o.Spec.Version, Paused: o.Spec.Paused,
		Replicas: 1, Shards: 1, Available: o.Status.AvailableReplicas, Conditions: []promOperatorCondition{},
	}

	// The operator's defaults: one replica, one shard.
	if o.Spec.Replicas != nil {
		s.Replicas = max(0, *o.Spec.Replicas)
	}

	if shards && o.Spec.Shards != nil {
		s.Shards = max(1, *o.Spec.Shards)
	}

	s.Desired = s.Replicas * s.Shards

	conds := kubeConditions(o.Status.Conditions)
	for _, typ := range []string{"Available", "Reconciled"} {
		if c := conds.get(typ); c.Status != "" {
			s.Conditions = append(s.Conditions, promOperatorCondition{Type: typ, Status: c.Status, Reason: c.Reason, Message: clipUTF8(c.Message, promMaxRuleError)})
		}
	}

	available, reconciled := conds.get("Available").Status, conds.get("Reconciled").Status

	switch {
	case s.Desired > 0 && s.Available == 0, available == "False":
		s.Health = healthCritical
	case s.Available < s.Desired, available == "Degraded", reconciled == "False":
		s.Health = healthWarning
	default:
		s.Health = healthOK
	}

	return s
}

// listMetas lists the metadata of the objects at path, as a Table when the server can.
func listMetas(ctx context.Context, k *kubeClient, path string) ([]kubeRowMeta, error) {
	metas := []kubeRowMeta{}

	err := k.listAll(ctx, path, pageQuery{table: true}, func() { metas = metas[:0] }, func(page kubePage) error {
		if page.table != nil {
			for _, row := range page.table.Rows {
				metas = append(metas, row.Object.Metadata)
			}

			return nil
		}

		for _, raw := range page.items {
			var obj struct {
				Metadata kubeRowMeta `json:"metadata"`
			}

			if err := json.Unmarshal(raw, &obj); err != nil {
				return err
			}

			metas = append(metas, obj.Metadata)
		}

		return nil
	})

	return metas, err
}

// countObjects counts the objects at path.
func countObjects(ctx context.Context, k *kubeClient, path string) (int, error) {
	metas, err := listMetas(ctx, k, path)

	return len(metas), err
}
