package ichorgo

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	cnpgBase        = "/apis/postgresql.cnpg.io/v1/namespaces/db/"
	cnpgClusterPath = cnpgBase + "clusters/pg"
	cnpgBackupsPath = cnpgBase + "backups"
)

var cnpgBackupTime = time.Date(2026, 10, 5, 12, 30, 45, 0, time.UTC)

func cnpgBackupAPI(t *testing.T, cluster, backups string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":               `{"groups":[{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + cnpgClusterPath:  cluster,
		"GET " + cnpgBackupsPath:  backups,
		"POST " + cnpgBackupsPath: `{"metadata":{"name":"pg-20261005123045"}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func TestBackupCNPGClusterMethods(t *testing.T) {
	cases := map[string]struct{ cluster, want string }{
		"plugin": {
			`{"spec":{"plugins":[{"name":"barman-cloud.cloudnative-pg.io","parameters":{"barmanObjectName":"s3"}}],"backup":{"barmanObjectStore":{"destinationPath":"s3://old"}}}}`,
			`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Backup","metadata":{"name":"pg-20261005123045","namespace":"db"},"spec":{"cluster":{"name":"pg"},"method":"plugin","pluginConfiguration":{"name":"barman-cloud.cloudnative-pg.io"}}}`,
		},
		"disabled plugin falls back to in-tree": {
			`{"spec":{"plugins":[{"name":"barman-cloud.cloudnative-pg.io","enabled":false}],"backup":{"barmanObjectStore":{"destinationPath":"s3://b"}}}}`,
			`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Backup","metadata":{"name":"pg-20261005123045","namespace":"db"},"spec":{"cluster":{"name":"pg"},"method":"barmanObjectStore"}}`,
		},
		"volume snapshot": {
			`{"spec":{"backup":{"volumeSnapshot":{"className":"csi"}}}}`,
			`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Backup","metadata":{"name":"pg-20261005123045","namespace":"db"},"spec":{"cluster":{"name":"pg"},"method":"volumeSnapshot"}}`,
		},
	}

	for label, tc := range cases {
		f, k := cnpgBackupAPI(t, tc.cluster, `{"items":[{"spec":{"cluster":{"name":"pg"}},"status":{"phase":"completed"}}]}`)

		got, err := backupCNPGCluster(context.Background(), k, "db", "pg", cnpgBackupTime)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}

		if got != "pg-20261005123045" {
			t.Errorf("%s: name %q", label, got)
		}

		last := lastRequest(f)
		if last.method != "POST" || last.path != cnpgBackupsPath || last.body != tc.want {
			t.Errorf("%s: %+v", label, last)
		}
	}
}

func TestBackupCNPGClusterRefusals(t *testing.T) {
	plugin := `"plugins":[{"name":"barman-cloud.cloudnative-pg.io"}]`
	cases := map[string]struct {
		cluster, backups string
		want             error
	}{
		"hibernated":      {`{"metadata":{"annotations":{"cnpg.io/hibernation":"on"}},"spec":{` + plugin + `}}`, `{"items":[]}`, errCNPGHibernated},
		"no backup":       {`{"spec":{"backup":{"barmanObjectStore":null}}}`, `{"items":[]}`, errCNPGNoBackup},
		"already running": {`{"spec":{` + plugin + `}}`, `{"items":[{"spec":{"cluster":{"name":"pg"}},"status":{"phase":"running"}}]}`, errCNPGBackingUp},
	}

	for label, tc := range cases {
		f, k := cnpgBackupAPI(t, tc.cluster, tc.backups)

		if _, err := backupCNPGCluster(context.Background(), k, "db", "pg", cnpgBackupTime); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v", label, err)
		}

		if last := lastRequest(f); last.method != "GET" {
			t.Errorf("%s: nothing may be created: %+v", label, last)
		}
	}
}

func TestBackupCNPGClusterWithoutCNPG(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := backupCNPGCluster(context.Background(), k, "db", "pg", cnpgBackupTime); !errors.Is(err, errCNPGMissing) {
		t.Fatalf("got %v", err)
	}
}
