package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

type longhornSalvageReplica struct {
	Name      string `json:"name"`
	Node      string `json:"node"`
	HealthyAt int64  `json:"healthyAt"`
	FailedAt  int64  `json:"failedAt"`
	Eligible  bool   `json:"eligible"`
}
type longhornSalvagePreview struct {
	Namespace  string                   `json:"namespace"`
	Name       string                   `json:"name"`
	State      string                   `json:"state"`
	Robustness string                   `json:"robustness"`
	CanSalvage bool                     `json:"canSalvage"`
	Error      string                   `json:"error"`
	Replicas   []longhornSalvageReplica `json:"replicas"`
}

// KubeLonghornSalvagePreview reads the volume and replica/node state without selecting replicas.
func KubeLonghornSalvagePreview(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	if err := validateKubeName("volume", namespace, name); err != nil {
		return "", err
	}
	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() longhornSalvagePreview {
		p := longhornSalvagePreview{Namespace: namespace, Name: name, Replicas: []longhornSalvageReplica{}}
		for _, v := range demoDataServices(time.Now()).Longhorn.Volumes {
			if v.Namespace == namespace && v.Name == name {
				p.State, p.Robustness = v.State, v.Robustness
				p.CanSalvage = v.State == "detached" && v.Robustness == "faulted"
				for i, node := range v.ReplicaNodes {
					p.Replicas = append(p.Replicas, longhornSalvageReplica{Name: fmt.Sprintf("%s-r%d", name, i+1), Node: node, Eligible: p.CanSalvage})
				}
				return p
			}
		}
		p.Error = "the demo volume was not found"
		return p
	}, func(ctx context.Context, k *kubeClient) (longhornSalvagePreview, error) {
		return readLonghornSalvagePreview(ctx, k, namespace, name)
	})
}

// KubeLonghornSalvage requests salvage through the supported manager API (os:admin).
func KubeLonghornSalvage(configYAML, contextName, kubeServer, namespace, name, replicasJSON string) (err error) {
	defer maskErr(&err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	var names []string
	if err = json.Unmarshal([]byte(replicasJSON), &names); err != nil {
		return errors.New("replicas must be a JSON array of names")
	}
	for i := range names {
		names[i] = privacy.reveal(strings.TrimSpace(names[i]))
		if err := validateKubeName("replica", namespace, names[i]); err != nil {
			return err
		}
	}
	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "longhorn-salvage", Namespace: namespace, Object: name, Params: "replicas=" + strings.Join(names, ",")})
	if err := validateKubeName("volume", namespace, name); err != nil {
		return err
	}
	if err := validateSalvageSelection(names); err != nil {
		return err
	}
	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error { return salvageLonghorn(ctx, k, namespace, name, names) })
}
func longhornObjectBase(ctx context.Context, k *kubeClient, namespace string) (string, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return "", err
	}
	version, ok := groups[groupLonghorn]
	if !ok {
		return "", errLonghornMissing
	}
	return "/apis/" + groupLonghorn + "/" + version + "/namespaces/" + url.PathEscape(namespace), nil
}
func readLonghornSalvagePreview(ctx context.Context, k *kubeClient, namespace, name string) (longhornSalvagePreview, error) {
	p := longhornSalvagePreview{Namespace: namespace, Name: name, Replicas: []longhornSalvageReplica{}}
	base, err := longhornObjectBase(ctx, k, namespace)
	if err != nil {
		return p, err
	}
	var v lhVolumeObject
	if err := k.get(ctx, base+"/volumes/"+url.PathEscape(name), &v); err != nil {
		return p, err
	}
	p.State, p.Robustness = v.Status.State, v.Status.Robustness
	var replicas kubeList[lhReplicaObject]
	var nodes kubeList[lhNodeObject]
	if err := getList(ctx, k, base+"/replicas", &replicas); err != nil {
		p.Error = err.Error()
		return p, nil
	}
	if err := getList(ctx, k, base+"/nodes", &nodes); err != nil {
		p.Error = err.Error()
		return p, nil
	}
	byNode := map[string]lhNodeObject{}
	for _, n := range nodes.Items {
		byNode[n.Metadata.Name] = n
	}
	for _, r := range replicas.Items {
		if r.Spec.VolumeName != name {
			continue
		}
		n, exists := byNode[r.Spec.NodeID]
		diskReady := false
		for _, d := range n.Status.DiskStatus {
			if r.Spec.DiskID != "" && d.DiskUUID == r.Spec.DiskID && conditionStatus(d.Conditions, "Schedulable") == "True" {
				diskReady = true
			}
		}
		times := r.times()
		eligible := exists && conditionStatus(n.Status.Conditions, "Ready") == "True" && diskReady && r.Metadata.Name != "" && r.Metadata.DeletionTimestamp == ""
		p.Replicas = append(p.Replicas, longhornSalvageReplica{Name: r.Metadata.Name, Node: r.Spec.NodeID, HealthyAt: unixMilli(times.HealthyAt), FailedAt: unixMilli(times.FailedAt), Eligible: eligible})
	}
	slices.SortFunc(p.Replicas, func(a, b longhornSalvageReplica) int { return strings.Compare(a.Name, b.Name) })
	p.CanSalvage = p.State == "detached" && p.Robustness == "faulted" && slices.ContainsFunc(p.Replicas, func(r longhornSalvageReplica) bool { return r.Eligible })
	return p, nil
}
func validateSalvageSelection(names []string) error {
	if len(names) == 0 {
		return errors.New("choose at least one replica explicitly")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			return errors.New("replica names must be nonempty and unique")
		}
		seen[name] = true
	}
	return nil
}
func salvageLonghorn(ctx context.Context, k *kubeClient, namespace, name string, names []string) error {
	if err := validateSalvageSelection(names); err != nil {
		return err
	}
	p, err := readLonghornSalvagePreview(ctx, k, namespace, name)
	if err != nil {
		return err
	}
	if p.Error != "" {
		return errors.New(p.Error)
	}
	if !p.CanSalvage {
		return errors.New("salvage requires a detached faulted volume and available replicas")
	}
	for _, name := range names {
		if !slices.ContainsFunc(p.Replicas, func(r longhornSalvageReplica) bool { return r.Name == name && r.Eligible }) {
			return fmt.Errorf("replica %q does not belong to this volume or is unavailable", name)
		}
	}
	// The manager validates state, replica ownership and node/disk availability again at submission.
	return k.post(ctx, serviceProxyPath(namespace, lhManagerService, "/v1/volumes/"+url.PathEscape(name)+"?action=salvage"), map[string]any{"replicaNames": names}, nil)
}
