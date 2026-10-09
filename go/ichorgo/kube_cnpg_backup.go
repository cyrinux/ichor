package ichorgo

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// On-demand CloudNativePG backup, the way `kubectl cnpg backup CLUSTER` does it: a Backup
// object for the cluster, with the method the cluster is set up for. The operator takes it
// from there; the cluster's lastSuccessfulBackup moves once it is done.

// Backup phases during which another backup of the same cluster would only queue up.
var cnpgBackupRunning = map[string]bool{"pending": true, "started": true, "running": true, "finalizing": true}

var (
	errCNPGMissing    = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "CloudNativePG is not installed"}
	errCNPGHibernated = &kubeAPIError{Code: 409, Reason: "Hibernated", Message: "the cluster is hibernated: no instance to back up"}
	errCNPGNoBackup   = &kubeAPIError{Code: 409, Reason: "NoBackupConfigured", Message: "the cluster has no backup configured (barman-cloud plugin, barmanObjectStore or volumeSnapshot)"}
	errCNPGBackingUp  = &kubeAPIError{Code: 409, Reason: "BackupRunning", Message: "a backup of this cluster is already running"}
)

// cnpgBackupNameTime suffixes the Backup name with when it was asked, as the cnpg plugin does.
const cnpgBackupNameTime = "20060102150405"

// cnpgBackupClusterObject is what a backup reads of a Cluster.
type cnpgBackupClusterObject struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Plugins []struct {
			Name    string `json:"name"`
			Enabled *bool  `json:"enabled"`
		} `json:"plugins"`
		Backup struct {
			BarmanObjectStore json.RawMessage `json:"barmanObjectStore"`
			VolumeSnapshot    json.RawMessage `json:"volumeSnapshot"`
		} `json:"backup"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

type cnpgBackupObject struct {
	Spec struct {
		Cluster struct {
			Name string `json:"name"`
		} `json:"cluster"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// KubeCNPGBackup starts a backup of the CloudNativePG cluster namespace/name now (os:admin),
// like `kubectl cnpg backup`. Refused for a hibernated cluster, one without a backup method,
// or one already being backed up. Returns the new Backup's name. kubeServer: see KubePods.
func KubeCNPGBackup(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "cnpg-backup", Namespace: namespace, Object: "Cluster/" + name})

	if err := validateKubeName("cluster", namespace, name); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		backup, err := backupCNPGCluster(ctx, k, namespace, name, time.Now())
		out = backup

		return err
	})

	return out, err
}

func backupCNPGCluster(ctx context.Context, k *kubeClient, namespace, name string, now time.Time) (string, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return "", err
	}

	version, ok := groups[groupCNPG]
	if !ok {
		return "", errCNPGMissing
	}

	base := "/apis/" + groupCNPG + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/"

	var c cnpgBackupClusterObject
	if err := k.get(ctx, base+"clusters/"+url.PathEscape(name), &c); err != nil {
		return "", err
	}

	if c.Metadata.Annotations["cnpg.io/hibernation"] == "on" || conditionStatus(c.Status.Conditions, "cnpg.io/hibernation") == "True" {
		return "", errCNPGHibernated
	}

	spec, err := cnpgBackupSpec(c, name)
	if err != nil {
		return "", err
	}

	var backups kubeList[cnpgBackupObject]
	if err := getList(ctx, k, base+"backups?labelSelector="+url.QueryEscape("cnpg.io/cluster="+name), &backups); err != nil {
		return "", err
	}

	for _, b := range backups.Items {
		if b.Spec.Cluster.Name == name && cnpgBackupRunning[b.Status.Phase] {
			return "", errCNPGBackingUp
		}
	}

	backup := map[string]any{
		"apiVersion": groupCNPG + "/" + version,
		"kind":       "Backup",
		"metadata": map[string]any{
			"name":      name + "-" + now.UTC().Format(cnpgBackupNameTime),
			"namespace": namespace,
		},
		"spec": spec,
	}

	var created struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := k.post(ctx, base+"backups", backup, &created); err != nil {
		return "", err
	}

	return created.Metadata.Name, nil
}

// cnpgBackupSpec picks the method the cluster is set up for: the barman-cloud plugin first,
// as it replaces the in-tree barmanObjectStore, then that one, then volume snapshots.
func cnpgBackupSpec(c cnpgBackupClusterObject, name string) (map[string]any, error) {
	spec := map[string]any{"cluster": map[string]any{"name": name}}

	for _, p := range c.Spec.Plugins {
		if p.Name == barmanCloudPlugin && (p.Enabled == nil || *p.Enabled) {
			spec["method"] = "plugin"
			spec["pluginConfiguration"] = map[string]any{"name": barmanCloudPlugin}

			return spec, nil
		}
	}

	switch {
	case rawSet(c.Spec.Backup.BarmanObjectStore):
		spec["method"] = "barmanObjectStore"
	case rawSet(c.Spec.Backup.VolumeSnapshot):
		spec["method"] = "volumeSnapshot"
	default:
		return nil, errCNPGNoBackup
	}

	return spec, nil
}

func rawSet(r json.RawMessage) bool {
	return len(r) > 0 && string(r) != "null"
}
