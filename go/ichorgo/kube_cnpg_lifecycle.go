package ichorgo

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

const cnpgPrimaryRestartPhase = "Primary instance is being restarted in-place"

type cnpgPrimaryPod struct {
	Metadata struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		UID               string            `json:"uid"`
		Labels            map[string]string `json:"labels"`
		DeletionTimestamp string            `json:"deletionTimestamp"`
		OwnerReferences   []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Status struct {
		Phase      string          `json:"phase"`
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

func cnpgLifecycleNames(config, contextName, namespace, name string) (string, string, string, error) {
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	return contextName, namespace, name, validateKubeName("cluster", namespace, name)
}

// KubeCNPGRestartPrimary asks CNPG to restart its current primary in place (os:admin).
func KubeCNPGRestartPrimary(configYAML, contextName, kubeServer, namespace, name string) (err error) {
	defer maskErr(&err)
	contextName, namespace, name, err = cnpgLifecycleNames(configYAML, contextName, namespace, name)
	contextName = unmaskContext(configYAML, contextName)
	if err != nil {
		return err
	}
	action := auditAction{Server: kubeServer, Action: "cnpg-restart-primary", Namespace: namespace, Object: "Cluster/" + name}
	defer func() { recordAction(&err, configYAML, contextName, action) }()
	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		var primary string
		primary, err = restartCNPGPrimary(ctx, k, namespace, name)
		action.Params = "primary=" + primary
		return err
	})
}

// KubeCNPGHibernate changes the declarative hibernation annotation; CNPG retains its PVCs.
func KubeCNPGHibernate(configYAML, contextName, kubeServer, namespace, name string, enabled bool) (err error) {
	defer maskErr(&err)
	contextName, namespace, name, err = cnpgLifecycleNames(configYAML, contextName, namespace, name)
	contextName = unmaskContext(configYAML, contextName)
	if err != nil {
		return err
	}
	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "cnpg-hibernate", Namespace: namespace, Object: "Cluster/" + name, Params: "enabled=" + strconv.FormatBool(enabled)})
	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error { return hibernateCNPG(ctx, k, namespace, name, enabled) })
}
func readCNPGLifecycleCluster(ctx context.Context, k *kubeClient, namespace, name string) (cnpgClusterObject, string, error) {
	var c cnpgClusterObject
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return c, "", err
	}
	version, ok := groups[groupCNPG]
	if !ok {
		return c, "", errCNPGMissing
	}
	path := "/apis/" + groupCNPG + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/clusters/" + url.PathEscape(name)
	if err := k.get(ctx, path, &c); err != nil {
		return c, "", err
	}
	if c.Metadata.ResourceVersion == "" {
		return c, "", errors.New("the cluster has no resource version")
	}
	return c, path, nil
}
func restartCNPGPrimary(ctx context.Context, k *kubeClient, namespace, name string) (string, error) {
	c, path, err := readCNPGLifecycleCluster(ctx, k, namespace, name)
	if err != nil {
		return "", err
	}
	primary := c.Status.CurrentPrimary
	if primary == "" {
		return "", errors.New("the cluster has no current primary")
	}
	if c.Metadata.Annotations["cnpg.io/hibernation"] == "on" || conditionStatus(c.Status.Conditions, "cnpg.io/hibernation") == "True" {
		return primary, errCNPGHibernated
	}
	if c.Status.Phase == cnpgPrimaryRestartPhase {
		return primary, errors.New("the primary restart is already requested")
	}
	if (c.Status.TargetPrimary != "" && primary != c.Status.TargetPrimary) || strings.Contains(strings.ToLower(c.Status.Phase), "switchover") || strings.Contains(strings.ToLower(c.Status.Phase), "failover") {
		return primary, errors.New("the cluster is changing its primary")
	}
	var p cnpgPrimaryPod
	if err := k.get(ctx, scopedPath("/api/v1", namespace, "pods")+"/"+url.PathEscape(primary), &p); err != nil {
		return primary, err
	}
	owned := false
	for _, owner := range p.Metadata.OwnerReferences {
		if owner.Kind == "Cluster" && owner.Name == name && c.Metadata.UID != "" && owner.UID == c.Metadata.UID {
			owned = true
		}
	}
	role := p.Metadata.Labels["cnpg.io/instanceRole"]
	if role == "" {
		role = p.Metadata.Labels["role"]
	}
	if !owned || p.Metadata.Name != primary || p.Metadata.Namespace != namespace || p.Metadata.UID == "" || p.Metadata.Labels["cnpg.io/cluster"] != name || (role != "" && role != "primary") {
		return primary, errors.New("the current primary Pod does not belong to this cluster or is not primary")
	}
	if p.Metadata.DeletionTimestamp != "" || p.Status.Phase != "Running" || conditionStatus(p.Status.Conditions, "Ready") != "True" {
		return primary, errors.New("the current primary Pod is not ready")
	}
	err = k.patch(ctx, path+"/status", "application/merge-patch+json", map[string]any{
		"metadata": map[string]any{"resourceVersion": c.Metadata.ResourceVersion},
		"status":   map[string]any{"phase": cnpgPrimaryRestartPhase, "phaseReason": "Requested by the user"},
	}, nil)
	return primary, err
}
func hibernateCNPG(ctx context.Context, k *kubeClient, namespace, name string, enabled bool) error {
	c, path, err := readCNPGLifecycleCluster(ctx, k, namespace, name)
	if err != nil {
		return err
	}
	currentlyRequested := c.Metadata.Annotations["cnpg.io/hibernation"] == "on"
	if enabled == currentlyRequested {
		return errors.New("the requested hibernation state is already set")
	}
	if c.Status.Phase == cnpgPrimaryRestartPhase || strings.Contains(strings.ToLower(c.Status.Phase), "switchover") || strings.Contains(strings.ToLower(c.Status.Phase), "failover") || (c.Status.TargetPrimary != "" && c.Status.CurrentPrimary != "" && c.Status.CurrentPrimary != c.Status.TargetPrimary) {
		return errors.New("the cluster is changing its primary")
	}
	value := "off"
	if enabled {
		value = "on"
	}
	return k.patch(ctx, path, "application/merge-patch+json", map[string]any{
		"metadata": map[string]any{"resourceVersion": c.Metadata.ResourceVersion, "annotations": map[string]any{"cnpg.io/hibernation": value}},
	}, nil)
}
