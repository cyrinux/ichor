package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type cnpgPromotePreview struct {
	Namespace      string   `json:"namespace"`
	Name           string   `json:"name"`
	CurrentPrimary string   `json:"currentPrimary"`
	Instance       string   `json:"instance"`
	LagBytes       *int64   `json:"lagBytes"`
	LagError       string   `json:"lagError"`
	Ready          bool     `json:"ready"`
	CanPromote     bool     `json:"canPromote"`
	Warnings       []string `json:"warnings"`
}
type cnpgActionCluster struct {
	Metadata struct {
		Name            string            `json:"name"`
		UID             string            `json:"uid"`
		ResourceVersion string            `json:"resourceVersion"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		CurrentPrimary string          `json:"currentPrimary"`
		TargetPrimary  string          `json:"targetPrimary"`
		Phase          string          `json:"phase"`
		Conditions     []kubeCondition `json:"conditions"`
	} `json:"status"`
}
type cnpgActionPod struct {
	Metadata struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		UID               string            `json:"uid"`
		Labels            map[string]string `json:"labels"`
		DeletionTimestamp *time.Time        `json:"deletionTimestamp"`
		OwnerReferences   []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			UID  string `json:"uid"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Status struct {
		Phase      string          `json:"phase"`
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}
type cnpgInstanceStatus struct {
	CurrentLsn         json.RawMessage `json:"currentLsn"`
	ReplayLsn          json.RawMessage `json:"replayLsn"`
	TimeLineID         int             `json:"timeLineID"`
	IsPrimary          *bool           `json:"isPrimary"`
	ReplayPaused       bool            `json:"replayPaused"`
	MightBeUnavailable bool            `json:"mightBeUnavailable"`
}

func cnpgActionNames(config, contextName, namespace, name, instance string) (string, string, string, string, error) {
	namespace, name, instance = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name)), privacy.reveal(strings.TrimSpace(instance))
	if err := validateKubeName("cluster", namespace, name); err != nil {
		return "", "", "", "", err
	}
	if err := validateKubeName("instance", namespace, instance); err != nil {
		return "", "", "", "", err
	}
	return contextName, namespace, name, instance, nil
}

// KubeCNPGPromotePreview reads a fresh primary/replica preview. Unknown lag is null.
func KubeCNPGPromotePreview(configYAML, contextName, kubeServer, namespace, name, instance string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName, namespace, name, instance, err = cnpgActionNames(configYAML, contextName, namespace, name, instance)
	contextName = unmaskContext(configYAML, contextName)
	if err != nil {
		return "", err
	}
	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() cnpgPromotePreview {
		return demoCNPGPromotePreview(namespace, name, instance)
	}, func(ctx context.Context, k *kubeClient) (cnpgPromotePreview, error) {
		return readCNPGPromotePreview(ctx, k, namespace, name, instance)
	})
}

func demoCNPGPromotePreview(namespace, name, instance string) cnpgPromotePreview {
	r := cnpgPromotePreview{Namespace: namespace, Name: name, Instance: instance, Warnings: []string{"notReady"}}
	for _, c := range demoDataServices(time.Now()).CNPG.Clusters {
		if c.Namespace != namespace || c.Name != name {
			continue
		}
		r.CurrentPrimary = c.CurrentPrimary
		for _, p := range c.InstancePods {
			if p.Name == instance {
				r.Ready = p.Ready
			}
		}
		r.Warnings = []string{}
		if !r.Ready {
			r.Warnings = append(r.Warnings, "notReady")
		}
		if c.CurrentPrimary == instance {
			r.Warnings = append(r.Warnings, "samePrimary")
		}
		if c.Hibernated {
			r.Warnings = append(r.Warnings, "hibernated")
		}
		if c.TargetPrimary != "" && c.CurrentPrimary != c.TargetPrimary {
			r.Warnings = append(r.Warnings, "switching")
		}
		r.CanPromote = len(r.Warnings) == 0
		if r.CanPromote {
			lag := int64(256)
			r.LagBytes = &lag
			r.Warnings = append(r.Warnings, "lagNonzero")
		}
		return r
	}
	return r
}

// KubeCNPGPromote requests an operator-managed switchover to a verified ready replica.
func KubeCNPGPromote(configYAML, contextName, kubeServer, namespace, name, instance string) (err error) {
	defer maskErr(&err)
	contextName, namespace, name, instance, err = cnpgActionNames(configYAML, contextName, namespace, name, instance)
	contextName = unmaskContext(configYAML, contextName)
	if err != nil {
		return err
	}
	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "cnpg-promote", Namespace: namespace, Object: "Cluster/" + name, Params: "instance=" + instance})
	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return promoteCNPG(ctx, k, namespace, name, instance, time.Now())
	})
}

func readCNPGActionCluster(ctx context.Context, k *kubeClient, namespace, name string) (cnpgActionCluster, string, error) {
	var c cnpgActionCluster
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
	return c, path, nil
}
func readCNPGActionPod(ctx context.Context, k *kubeClient, c cnpgActionCluster, namespace, name, instance string) (cnpgActionPod, error) {
	var p cnpgActionPod
	if err := k.get(ctx, scopedPath("/api/v1", namespace, "pods")+"/"+url.PathEscape(instance), &p); err != nil {
		return p, err
	}
	owned := false
	for _, owner := range p.Metadata.OwnerReferences {
		if owner.Kind == "Cluster" && owner.Name == name && c.Metadata.UID != "" && owner.UID == c.Metadata.UID {
			owned = true
		}
	}
	if !owned || p.Metadata.Labels["cnpg.io/cluster"] != name || p.Metadata.Name != instance || p.Metadata.Namespace != namespace {
		return p, errors.New("the target instance does not belong to this CloudNativePG cluster")
	}
	return p, nil
}
func cnpgPodReady(p cnpgActionPod) bool {
	return p.Metadata.DeletionTimestamp == nil && p.Status.Phase == "Running" && conditionStatus(p.Status.Conditions, "Ready") == "True"
}
func cnpgPromotionWarnings(c cnpgActionCluster, p cnpgActionPod, instance string) ([]string, error) {
	warnings := []string{}
	if !cnpgPodReady(p) {
		warnings = append(warnings, "notReady")
	}
	if c.Status.CurrentPrimary == instance || p.Metadata.Labels["role"] == "primary" || p.Metadata.Labels["cnpg.io/instanceRole"] == "primary" {
		warnings = append(warnings, "samePrimary")
	}
	if c.Metadata.Annotations["cnpg.io/hibernation"] == "on" || conditionStatus(c.Status.Conditions, "cnpg.io/hibernation") == "True" {
		warnings = append(warnings, "hibernated")
	}
	if c.Status.CurrentPrimary == "" || (c.Status.TargetPrimary != "" && c.Status.CurrentPrimary != c.Status.TargetPrimary) || strings.Contains(strings.ToLower(c.Status.Phase), "switchover") || strings.Contains(strings.ToLower(c.Status.Phase), "failover") || c.Status.Phase == cnpgPrimaryRestartPhase {
		warnings = append(warnings, "switching")
	}
	if raw := c.Metadata.Annotations["cnpg.io/fencedInstances"]; raw != "" {
		var fenced []string
		if err := json.Unmarshal([]byte(raw), &fenced); err != nil {
			return nil, errors.New("cannot read the cluster's fenced instances")
		}
		if slices.Contains(fenced, instance) || slices.Contains(fenced, "*") {
			warnings = append(warnings, "fenced")
		}
	}
	return warnings, nil
}
func readCNPGPromotePreview(ctx context.Context, k *kubeClient, namespace, name, instance string) (cnpgPromotePreview, error) {
	r := cnpgPromotePreview{Namespace: namespace, Name: name, Instance: instance, Warnings: []string{}}
	c, _, err := readCNPGActionCluster(ctx, k, namespace, name)
	if err != nil {
		return r, err
	}
	p, err := readCNPGActionPod(ctx, k, c, namespace, name, instance)
	if err != nil {
		return r, err
	}
	r.CurrentPrimary = c.Status.CurrentPrimary
	r.Ready = cnpgPodReady(p)
	r.Warnings, err = cnpgPromotionWarnings(c, p, instance)
	if err != nil {
		return r, err
	}
	r.CanPromote = len(r.Warnings) == 0
	if r.CanPromote {
		lagCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		r.LagBytes, err = readCNPGReplicationLag(lagCtx, k, namespace, c.Status.CurrentPrimary, instance)
		if err != nil {
			r.LagError = err.Error()
			r.Warnings = append(r.Warnings, "lagUnknown")
		} else if *r.LagBytes > 0 {
			r.Warnings = append(r.Warnings, "lagNonzero")
		}
	}
	return r, nil
}
func readCNPGInstanceStatus(ctx context.Context, k *kubeClient, namespace, pod string) (cnpgInstanceStatus, error) {
	var s cnpgInstanceStatus
	base := scopedPath("/api/v1", namespace, "pods") + "/"
	// New releases serve TLS; older releases served HTTP on the same status port.
	err := k.get(ctx, base+"https:"+url.PathEscape(pod)+":8000/proxy/pg/status", &s)
	if err != nil && kubeCode(err) != 403 {
		err = k.get(ctx, base+url.PathEscape(pod)+":8000/proxy/pg/status", &s)
	}
	return s, err
}
func readCNPGReplicationLag(ctx context.Context, k *kubeClient, namespace, primary, instance string) (*int64, error) {
	writer, err := readCNPGInstanceStatus(ctx, k, namespace, primary)
	if err != nil {
		return nil, err
	}
	replica, err := readCNPGInstanceStatus(ctx, k, namespace, instance)
	if err != nil {
		return nil, err
	}
	write, writeOK := parseCNPGLSN(writer.CurrentLsn)
	replay, replayOK := parseCNPGLSN(replica.ReplayLsn)
	if !writeOK || !replayOK || writer.IsPrimary == nil || !*writer.IsPrimary || replica.IsPrimary == nil || *replica.IsPrimary || writer.MightBeUnavailable || replica.MightBeUnavailable || replica.ReplayPaused || writer.TimeLineID == 0 || writer.TimeLineID != replica.TimeLineID || replay > write || write-replay > uint64(1<<63-1) {
		return nil, errors.New("replication lag is unavailable or the instance timelines do not match")
	}
	lag := int64(write - replay)
	return &lag, nil
}
func parseCNPGLSN(raw json.RawMessage) (uint64, bool) {
	var value string
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	if json.Unmarshal(raw, &value) != nil {
		n, err := strconv.ParseUint(string(raw), 10, 64)
		return n, err == nil
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0, false
	}
	high, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, false
	}
	low, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, false
	}
	return high<<32 | low, true
}
func promoteCNPG(ctx context.Context, k *kubeClient, namespace, name, instance string, now time.Time) error {
	c, path, err := readCNPGActionCluster(ctx, k, namespace, name)
	if err != nil {
		return err
	}
	p, err := readCNPGActionPod(ctx, k, c, namespace, name, instance)
	if err != nil {
		return err
	}
	warnings, err := cnpgPromotionWarnings(c, p, instance)
	if err != nil {
		return err
	}
	if len(warnings) > 0 {
		return fmt.Errorf("cannot promote this instance: %s", strings.Join(warnings, ", "))
	}
	if c.Metadata.ResourceVersion == "" {
		return errors.New("the cluster has no resource version")
	}
	return k.patch(ctx, path+"/status", "application/merge-patch+json", map[string]any{
		"metadata": map[string]any{"resourceVersion": c.Metadata.ResourceVersion},
		"status":   map[string]any{"targetPrimary": instance, "targetPrimaryTimestamp": now.UTC().Format(time.RFC3339), "phase": "Switchover in progress", "phaseReason": "Switching over to " + instance},
	}, nil)
}
