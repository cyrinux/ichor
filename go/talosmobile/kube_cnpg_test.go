package talosmobile

import (
	"testing"
	"time"
)

func TestCNPGHibernatedIsIdle(t *testing.T) {
	var c cnpgClusterObject
	c.Metadata.Namespace, c.Metadata.Name = "db", "pg"
	c.Metadata.Annotations = map[string]string{"cnpg.io/hibernation": "on"}
	c.Spec.Instances = 3

	got := mapCNPG([]cnpgClusterObject{c}, nil, nil, nil, fixtureNow).Clusters[0]
	if !got.Hibernated || got.Health != healthIdle || len(got.Reasons) != 0 {
		t.Fatalf("hibernated cluster: %+v", got)
	}
}

func TestCNPGInstancePodsOnly(t *testing.T) {
	var c cnpgClusterObject
	c.Metadata.Namespace, c.Metadata.Name = "db", "pg"
	c.Spec.Instances = 1
	c.Status.ReadyInstances = 1

	img := "ghcr.io/cloudnative-pg/postgresql:18"
	pods := []dsPod{
		fakePod("db", "pg-1", "n1", true, map[string]string{"cnpg.io/cluster": "pg", "cnpg.io/podRole": "instance", "cnpg.io/instanceRole": "primary"}, "postgres", img),
		fakePod("db", "pg-1-initdb-x", "n1", false, map[string]string{"cnpg.io/cluster": "pg", "cnpg.io/podRole": "initdb"}, "initdb", img),
		fakePod("db", "pg-2", "n2", true, map[string]string{"cnpg.io/cluster": "pg", "role": "replica"}, "postgres", img),
	}

	got := mapCNPG([]cnpgClusterObject{c}, nil, nil, pods, fixtureNow).Clusters[0]
	if len(got.InstancePods) != 2 || got.InstancePods[0].Role != "primary" || got.InstancePods[1].Role != "replica" {
		t.Fatalf("instance pods: %+v", got.InstancePods)
	}
}

func TestCronIntervalShortcuts(t *testing.T) {
	const day = 24 * time.Hour

	for schedule, want := range map[string]time.Duration{"@yearly": 366 * day, "@annually": 366 * day, "@hourly": day, "@every 6h": day} {
		if got := cronInterval(schedule); got != want {
			t.Errorf("%q: got %v, want %v", schedule, got, want)
		}
	}
}
