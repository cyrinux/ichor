package ichorgo

import (
	"errors"
	"testing"
	"time"
)

// The actions on one named object share kubeNamedMutation: their audit entries and
// validation messages are a contract with the apps and must not drift.
func TestKubeNamedMutationAuditAndValidation(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		call            func(namespace, name string) (string, error)
		action, object  string
		missingNameText string
	}{
		{func(ns, n string) (string, error) { return KubeCNPGBackup(demo, "Demo cluster", "", ns, n) }, "cnpg-backup", "Cluster/db", "no cluster namespace or name given"},
		{func(ns, n string) (string, error) { return KubeVeleroBackupNow(demo, "Demo cluster", "", ns, n) }, "velero-backup-now", "Schedule/db", "no schedule namespace or name given"},
		{func(ns, n string) (string, error) { return KubeVeleroDeleteBackup(demo, "Demo cluster", "", ns, n) }, "velero-delete-backup", "Backup/db", "no backup namespace or name given"},
		{func(ns, n string) (string, error) { return KubeTriggerCronJob(demo, "Demo cluster", "", ns, n) }, "trigger-cronjob", "CronJob/db", "no CronJob namespace or name given"},
		{func(ns, n string) (string, error) { return KubeCreateVolumeSnapshot(demo, "Demo cluster", "", ns, n) }, "create-volume-snapshot", "PersistentVolumeClaim/db", "no PVC namespace or name given"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			withDataDir(t)
			withAuditClock(t, time.Now())

			if _, err := tc.call(" web ", " db "); !errors.Is(err, errDemoUnavailable) {
				t.Fatalf("in the demo = %v", err)
			}

			got := readAudit(t, "", "")
			if len(got) != 1 {
				t.Fatalf("got %d entries, want 1", len(got))
			}

			if e := got[0]; e.Action != tc.action || e.Namespace != "web" || e.Object != tc.object || e.Outcome != auditFailed {
				t.Errorf("entry = %+v", e)
			}

			if _, err := tc.call("web", ""); err == nil || err.Error() != tc.missingNameText {
				t.Errorf("missing name = %v, want %q", err, tc.missingNameText)
			}
		})
	}
}
