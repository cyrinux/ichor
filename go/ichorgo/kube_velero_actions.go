package ichorgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Velero actions, the way the velero CLI does them: `velero backup create --from-schedule`
// posts a Backup copied from the schedule's template, `velero backup delete` posts a
// DeleteBackupRequest the server's deletion controller carries out (it removes the backup's
// data from the object store, then the Backup).

// The labels Velero sets and selects on.
const (
	veleroScheduleLabel  = "velero.io/schedule-name"
	veleroBackupLabel    = "velero.io/backup-name"
	veleroBackupUIDLabel = "velero.io/backup-uid"
	// veleroBackupNameTime suffixes a backup taken from a schedule, as the schedule's own do.
	veleroBackupNameTime = "20060102150405"
	// kubeLabelValueMax is the longest label value Kubernetes accepts.
	kubeLabelValueMax = 63
)

var (
	errVeleroPaused       = &kubeAPIError{Code: 409, Reason: "SchedulePaused", Message: "the schedule is paused: unpause it before backing it up"}
	errVeleroInvalid      = &kubeAPIError{Code: 409, Reason: "FailedValidation", Message: "the schedule failed validation: fix it before backing it up"}
	errVeleroBackingUp    = &kubeAPIError{Code: 409, Reason: "BackupRunning", Message: "a backup of this schedule is already running"}
	errVeleroStillRunning = &kubeAPIError{Code: 409, Reason: "BackupRunning", Message: "the backup is still running: it can be deleted once it is finished"}
	errVeleroDeleting     = &kubeAPIError{Code: 409, Reason: "DeletionPending", Message: "the backup is already being deleted"}
)

// veleroScheduleRaw is what backing a schedule up reads of it: the template is copied as is.
type veleroScheduleRaw struct {
	Metadata struct {
		Name        string            `json:"name"`
		UID         string            `json:"uid"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Paused                     bool            `json:"paused"`
		UseOwnerReferencesInBackup bool            `json:"useOwnerReferencesInBackup"`
		Template                   json.RawMessage `json:"template"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// KubeVeleroBackupNow backs the Velero schedule namespace/schedule up now (os:admin), like
// `velero backup create --from-schedule`: a Backup named <schedule>-<UTC time> with the
// schedule's template. Refused for a paused schedule, one that failed validation, or one
// with a backup running. Returns the new Backup's name. kubeServer: see KubePods.
func KubeVeleroBackupNow(configYAML, contextName, kubeServer, namespace, schedule string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, schedule = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(schedule))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "velero-backup-now", Namespace: namespace, Object: "Schedule/" + schedule})

	if err := validateKubeName("schedule", namespace, schedule); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		name, err := backupVeleroSchedule(ctx, k, namespace, schedule, time.Now())
		out = name

		return err
	})

	return out, err
}

func veleroNamespacePath(namespace string) string {
	return "/apis/" + groupVelero + "/" + veleroVersion + "/namespaces/" + url.PathEscape(namespace) + "/"
}

func backupVeleroSchedule(ctx context.Context, k *kubeClient, namespace, schedule string, now time.Time) (string, error) {
	base := veleroNamespacePath(namespace)

	var s veleroScheduleRaw
	if err := k.get(ctx, base+"schedules/"+url.PathEscape(schedule), &s); err != nil {
		return "", err
	}

	switch {
	case s.Status.Phase == "FailedValidation":
		return "", errVeleroInvalid
	case s.Spec.Paused:
		return "", errVeleroPaused
	}

	var running kubeList[veleroBackupObject]
	if err := getList(ctx, k, base+"backups?labelSelector="+url.QueryEscape(veleroScheduleLabel+"="+veleroLabelValue(schedule)), &running); err != nil {
		return "", err
	}

	for _, b := range running.Items {
		if veleroRunning(b.Status.Phase) {
			return "", errVeleroBackingUp
		}
	}

	backup, err := veleroBackupFromSchedule(s, namespace, now)
	if err != nil {
		return "", err
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

// veleroBackupFromSchedule is Velero's builder.FromSchedule: the schedule's labels and
// annotations, its name label, the template's own labels on top, the template as the spec,
// and an owner reference only when the schedule asks for one.
func veleroBackupFromSchedule(s veleroScheduleRaw, namespace string, now time.Time) (map[string]any, error) {
	spec := map[string]any{}
	if rawSet(s.Spec.Template) {
		if err := json.Unmarshal(s.Spec.Template, &spec); err != nil {
			return nil, err
		}
	}

	labels := map[string]any{}
	for k, v := range s.Metadata.Labels {
		labels[k] = v
	}

	labels[veleroScheduleLabel] = veleroLabelValue(s.Metadata.Name)

	if meta, ok := spec["metadata"].(map[string]any); ok {
		if tl, ok := meta["labels"].(map[string]any); ok {
			for k, v := range tl {
				labels[k] = v
			}
		}
	}

	annotations := map[string]any{}
	for k, v := range s.Metadata.Annotations {
		annotations[k] = v
	}

	metadata := map[string]any{
		"name":        s.Metadata.Name + "-" + now.UTC().Format(veleroBackupNameTime),
		"namespace":   namespace,
		"labels":      labels,
		"annotations": annotations,
	}

	if s.Spec.UseOwnerReferencesInBackup {
		metadata["ownerReferences"] = []map[string]any{{
			"apiVersion": groupVelero + "/" + veleroVersion, "kind": "Schedule",
			"name": s.Metadata.Name, "uid": s.Metadata.UID, "controller": true,
		}}
	}

	return map[string]any{
		"apiVersion": groupVelero + "/" + veleroVersion,
		"kind":       "Backup",
		"metadata":   metadata,
		"spec":       spec,
	}, nil
}

// veleroLabelValue is Velero's label.GetValidName: a name too long for a label value is cut
// and suffixed with the start of its SHA-256, so the controller finds it again.
func veleroLabelValue(name string) string {
	if len(name) <= kubeLabelValueMax {
		return name
	}

	sum := sha256.Sum256([]byte(name))

	return name[:kubeLabelValueMax-6] + hex.EncodeToString(sum[:])[:6]
}

// veleroDeleteRequestObject is a DeleteBackupRequest: New, InProgress, then Processed. The
// controller deletes a request that succeeded; one left Processed carries its errors.
type veleroDeleteRequestObject struct {
	Metadata struct {
		Name              string `json:"name"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		BackupName string `json:"backupName"`
	} `json:"spec"`
	Status struct {
		Phase  string   `json:"phase"`
		Errors []string `json:"errors"`
	} `json:"status"`
}

// pending tells whether the deletion is still to be carried out.
func (r veleroDeleteRequestObject) pending() bool {
	return r.Status.Phase == "" || r.Status.Phase == "New" || r.Status.Phase == "InProgress"
}

// KubeVeleroDeleteBackup asks Velero to delete the backup namespace/backup (os:admin), like
// `velero backup delete`: a DeleteBackupRequest, after which the server removes the backup's
// data from its storage location and the Backup itself. Refused while the backup runs or
// another deletion of it is pending. Returns the request's name; a deletion that fails shows
// its errors on the next KubeVeleroBackups. kubeServer: see KubePods.
func KubeVeleroDeleteBackup(configYAML, contextName, kubeServer, namespace, backup string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, backup = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(backup))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "velero-delete-backup", Namespace: namespace, Object: "Backup/" + backup})

	if err := validateKubeName("backup", namespace, backup); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		name, err := deleteVeleroBackup(ctx, k, namespace, backup)
		out = name

		return err
	})

	return out, err
}

func deleteVeleroBackup(ctx context.Context, k *kubeClient, namespace, backup string) (string, error) {
	base := veleroNamespacePath(namespace)

	var b struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := k.get(ctx, base+"backups/"+url.PathEscape(backup), &b); err != nil {
		return "", err
	}

	switch {
	case veleroRunning(b.Status.Phase):
		return "", errVeleroStillRunning
	case b.Status.Phase == "Deleting":
		return "", errVeleroDeleting
	}

	var requests kubeList[veleroDeleteRequestObject]
	if err := getList(ctx, k, base+"deletebackuprequests?labelSelector="+url.QueryEscape(veleroBackupLabel+"="+veleroLabelValue(backup)), &requests); err != nil {
		return "", err
	}

	for _, r := range requests.Items {
		if r.Spec.BackupName == backup && r.pending() {
			return "", errVeleroDeleting
		}
	}

	request := map[string]any{
		"apiVersion": groupVelero + "/" + veleroVersion,
		"kind":       "DeleteBackupRequest",
		"metadata": map[string]any{
			"generateName": backup + "-",
			"namespace":    namespace,
			"labels": map[string]any{
				veleroBackupLabel:    veleroLabelValue(backup),
				veleroBackupUIDLabel: b.Metadata.UID,
			},
		},
		"spec": map[string]any{"backupName": backup},
	}

	var created struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := k.post(ctx, base+"deletebackuprequests", request, &created); err != nil {
		return "", err
	}

	return created.Metadata.Name, nil
}
