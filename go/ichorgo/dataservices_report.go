package ichorgo

import (
	"fmt"
	"strings"
)

// The DATA SERVICES section of the diagnosis reads every service the same way: a count line,
// then its items with a health, a reason and the Kubernetes nodes involved (renderDataServices
// lists those that are not fine). One adapter per section of dataServices maps it to that.

type dsSection struct {
	name    string // "Longhorn"
	summary string // "42 volumes, 1 critical", "" when the section could not be read at all
	err     string
	issues  []dsIssue
}

type dsIssue struct {
	health string // healthCritical…healthIdle; anything else is not listed
	key    string // what it is: "db/pg", "node worker-3"
	detail string
	nodes  []string
}

// dataServiceSections maps the installed services, in the order of dataServices' fields.
func dataServiceSections(ds dataServices) []dsSection {
	adapters := []func(dataServices) (dsSection, bool){
		sealedSecretsSection, snapshotsSection, backupJobsSection, longhornSection, garageSection,
		cnpgSection, dragonflySection, mariadbSection, perconaSection, certManagerSection,
		veleroSection, externalDNSSection, castAISection,
	}

	var out []dsSection

	for _, adapt := range adapters {
		if sec, ok := adapt(ds); ok {
			out = append(out, sec)
		}
	}

	return out
}

// newDSSection counts items by health: "3 volumes, 1 critical, 1 idle". A section whose whole
// read failed (an error and no item) has no count line.
func newDSSection(name, noun, err string, items []dsIssue) dsSection {
	sec := dsSection{name: name, err: err, issues: items}
	if err != "" && len(items) == 0 {
		return sec
	}

	counts := map[string]int{}
	for _, i := range items {
		counts[i.health]++
	}

	sec.summary = fmt.Sprintf("%d %s", len(items), noun)

	for _, h := range []string{healthCritical, healthWarning, healthIdle} {
		if counts[h] > 0 {
			sec.summary += fmt.Sprintf(", %d %s", counts[h], h)
		}
	}

	return sec
}

// details joins the non-empty parts with "; ".
func details(parts ...string) string {
	return strings.Join(nonEmpty(parts), "; ")
}

func nonEmpty(parts []string) []string {
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}

	return out
}

func nsName(namespace, name string) string {
	if namespace == "" {
		return name
	}

	return namespace + "/" + name
}

func readyOf(ready, total int, what string) string {
	if total == 0 {
		return ""
	}

	return fmt.Sprintf("%d/%d %s", ready, total, what)
}

func sealedSecretsSection(ds dataServices) (dsSection, bool) {
	s := ds.SealedSecrets
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, dsIssue{health: it.Health, key: nsName(it.Namespace, it.Name), detail: details(strings.Join(it.Reasons, ", "), it.Message)})
	}

	sec := newDSSection("Sealed Secrets", "sealed secrets", s.Error, items)

	if c := s.Controller; c != nil {
		sec.issues = append(sec.issues, dsIssue{
			health: c.Health, key: "controller " + nsName(c.Namespace, c.Name),
			detail: details(strings.Join(c.Reasons, ", "), readyOf(c.ReadyReplicas, c.Replicas, "replicas ready")),
		})
	} else if s.Health == healthCritical || s.Health == healthWarning {
		// No key, no controller, a compromised or stale key: the section's own verdict.
		sec.issues = append(sec.issues, dsIssue{health: s.Health, key: "controller", detail: strings.Join(s.Reasons, ", ")})
	}

	return sec, true
}

func snapshotsSection(ds dataServices) (dsSection, bool) {
	s := ds.Snapshots
	if s == nil || (!s.Installed && s.Error == "") {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Snapshots))

	for _, snap := range s.Snapshots {
		health := healthOK
		if snap.Error != "" {
			health = healthWarning
		}

		items = append(items, dsIssue{health: health, key: nsName(snap.Namespace, snap.Name), detail: details("pvc "+snap.PVC, snap.Error)})
	}

	return newDSSection("Volume snapshots", "snapshots", details(s.Error, s.ContentsError, s.ClassesError, s.ClaimsError), items), true
}

func backupJobsSection(ds dataServices) (dsSection, bool) {
	s := ds.BackupJobs
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Jobs))

	for _, j := range s.Jobs {
		var streak string
		if j.FailureStreak > 0 {
			streak = fmt.Sprintf("%d failed runs in a row", j.FailureStreak)
		}

		items = append(items, dsIssue{health: j.Health, key: nsName(j.Namespace, j.Name), detail: details(streak)})
	}

	return newDSSection("Backup CronJobs", "jobs", details(s.Error, s.JobsError), items), true
}

func longhornSection(ds dataServices) (dsSection, bool) {
	s := ds.Longhorn
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Volumes))

	for _, v := range s.Volumes {
		key := v.Name
		if v.PVCName != "" {
			key += " (pvc " + nsName(v.PVCNamespace, v.PVCName) + ")"
		}

		state := strings.Join(nonEmpty([]string{v.State, v.Robustness}), ", ")
		if v.ReplicasDesired > 0 {
			state += fmt.Sprintf(", %d/%d replicas healthy", v.ReplicasHealthy, v.ReplicasDesired)
		}

		var attached string
		if v.Node != "" {
			attached = "attached to " + v.Node
		}

		items = append(items, dsIssue{health: v.Health, key: key, detail: details(state, attached, v.ScheduleError), nodes: v.ReplicaNodes})
	}

	sec := newDSSection("Longhorn", "volumes", s.Error, items)

	for _, n := range s.Nodes {
		if !n.Ready {
			sec.issues = append(sec.issues, dsIssue{health: healthCritical, key: "node " + n.Name, detail: "not ready"})
		}
	}

	for _, t := range s.BackupTargets {
		if !t.Available {
			sec.issues = append(sec.issues, dsIssue{health: healthWarning, key: "backup target " + t.Name, detail: details(t.Message)})
		}
	}

	return sec, true
}

func garageSection(ds dataServices) (dsSection, bool) {
	s := ds.Garage
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Instances))

	for _, in := range s.Instances {
		state := in.Status
		if in.StorageNodes > 0 {
			state += fmt.Sprintf(", %d/%d storage nodes up", in.StorageNodesUp, in.StorageNodes)
		}

		if in.Partitions > 0 {
			state += fmt.Sprintf(", %d/%d partitions with all nodes", in.PartitionsAllOk, in.Partitions)
		}

		var down []string

		for _, n := range in.Nodes {
			if !n.Up {
				down = append(down, orDefault(n.KubeNode, n.Hostname))
			}
		}

		items = append(items, dsIssue{health: garageHealth(in.Status), key: nsName(in.Namespace, in.Name), detail: details(state, in.Message), nodes: down})
	}

	sec := newDSSection("Garage", "instances", s.Error, items)

	for _, c := range s.Clusters {
		sec.issues = append(sec.issues, dsIssue{health: c.Health, key: "GarageCluster " + nsName(c.Namespace, c.Name), detail: details(c.Phase, strings.Join(c.Reasons, ", "))})
	}

	for _, bk := range s.Buckets {
		sec.issues = append(sec.issues, dsIssue{health: bk.Health, key: "GarageBucket " + nsName(bk.Namespace, bk.Name), detail: details(bk.Phase, strings.Join(bk.Reasons, ", "))})
	}

	for _, k := range s.Keys {
		sec.issues = append(sec.issues, dsIssue{health: k.Health, key: "GarageKey " + nsName(k.Namespace, k.Name), detail: details(k.Phase, strings.Join(k.Reasons, ", "))})
	}

	for _, n := range s.Nodes {
		sec.issues = append(sec.issues, dsIssue{health: n.Health, key: "GarageNode " + nsName(n.Namespace, n.Name), detail: details(n.Phase, strings.Join(n.Reasons, ", "))})
	}

	if op := s.Operator; op != nil {
		sec.issues = append(sec.issues, dsIssue{
			health: op.Health, key: "operator " + nsName(op.Namespace, op.Name),
			detail: details(strings.Join(op.Reasons, ", "), readyOf(op.Ready, op.Desired, "replicas ready")),
		})
	}

	return sec, true
}

// garageHealth maps a Garage instance's status to the data services' health.
func garageHealth(status string) string {
	switch status {
	case garageHealthy:
		return healthOK
	case garageUnavailable:
		return healthCritical
	default:
		return healthWarning
	}
}

func cnpgSection(ds dataServices) (dsSection, bool) {
	s := ds.CNPG
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Clusters))

	for _, c := range s.Clusters {
		nodes := make([]string, 0, len(c.InstancePods))

		for _, p := range c.InstancePods {
			node := orDefault(p.Node, "no node")
			if !p.Ready {
				node += " (" + p.Name + " not ready)"
			}

			nodes = append(nodes, node)
		}

		items = append(items, dsIssue{
			health: c.Health, key: nsName(c.Namespace, c.Name), nodes: nodes,
			detail: details(c.Phase, strings.Join(c.Reasons, ", "), readyOf(c.ReadyInstances, c.Instances, "instances ready")),
		})
	}

	return newDSSection("CloudNativePG", "clusters", s.Error, items), true
}

func dragonflySection(ds dataServices) (dsSection, bool) {
	s := ds.Dragonfly
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Instances))
	for _, in := range s.Instances {
		items = append(items, dsIssue{
			health: in.Health, key: nsName(in.Namespace, in.Name),
			detail: details(in.Phase, strings.Join(in.Reasons, ", "), readyOf(in.ReadyPods, in.Replicas, "pods ready")),
		})
	}

	return newDSSection("Dragonfly", "instances", s.Error, items), true
}

func mariadbSection(ds dataServices) (dsSection, bool) {
	s := ds.MariaDB
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Clusters))
	for _, c := range s.Clusters {
		items = append(items, dsIssue{
			health: c.Health, key: nsName(c.Namespace, c.Name),
			detail: details(strings.Join(c.Reasons, ", "), c.Message, readyOf(c.ReadyPods, c.Replicas, "pods ready")),
		})
	}

	return newDSSection("MariaDB", "clusters", s.Error, items), true
}

func perconaSection(ds dataServices) (dsSection, bool) {
	s := ds.Percona
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Clusters))
	for _, c := range s.Clusters {
		items = append(items, dsIssue{
			health: c.Health, key: nsName(c.Namespace, c.Name),
			detail: details(c.State, strings.Join(c.Reasons, ", "), c.Message, readyOf(c.PXCReady, c.PXCSize, "PXC members ready")),
		})
	}

	return newDSSection("Percona XtraDB", "clusters", s.Error, items), true
}

func certManagerSection(ds dataServices) (dsSection, bool) {
	s := ds.CertManager
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Certificates))
	for _, c := range s.Certificates {
		items = append(items, dsIssue{health: c.Health, key: nsName(c.Namespace, c.Name), detail: details(strings.Join(c.Reasons, ", "), c.Message, c.Issuer)})
	}

	sec := newDSSection("cert-manager", "certificates", s.Error, items)

	for _, i := range s.Issuers {
		sec.issues = append(sec.issues, dsIssue{health: i.Health, key: i.Kind + " " + nsName(i.Namespace, i.Name), detail: details(i.Message)})
	}

	return sec, true
}

func veleroSection(ds dataServices) (dsSection, bool) {
	s := ds.Velero
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Schedules))
	for _, sc := range s.Schedules {
		items = append(items, dsIssue{health: sc.Health, key: nsName(sc.Namespace, sc.Name), detail: details(strings.Join(sc.Reasons, ", "), strings.Join(sc.ValidationErrors, ", "))})
	}

	sec := newDSSection("Velero", "schedules", s.Error, items)

	for _, l := range s.Locations {
		sec.issues = append(sec.issues, dsIssue{health: l.Health, key: "location " + nsName(l.Namespace, l.Name), detail: details(l.Phase, l.Message)})
	}

	for _, a := range s.Adhoc {
		sec.issues = append(sec.issues, dsIssue{health: a.Health, key: "backup " + nsName(a.Namespace, a.Name), detail: details(a.Phase, a.FailureReason)})
	}

	return sec, true
}

func externalDNSSection(ds dataServices) (dsSection, bool) {
	s := ds.ExternalDNS
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Controllers))
	for _, c := range s.Controllers {
		items = append(items, dsIssue{
			health: c.Health, key: nsName(c.Namespace, c.Name),
			detail: details(strings.Join(c.Reasons, ", "), readyOf(c.ReadyReplicas, c.Replicas, "replicas ready")),
		})
	}

	sec := newDSSection("ExternalDNS", "controllers", s.Error, items)

	for _, rec := range s.Records {
		sec.issues = append(sec.issues, dsIssue{
			health: rec.Health, key: rec.RecordType + " " + rec.DNSName,
			detail: details(strings.Join(rec.Reasons, ", "), rec.SourceKind+" "+nsName(rec.SourceNamespace, rec.SourceName)),
		})
	}

	return sec, true
}

func castAISection(ds dataServices) (dsSection, bool) {
	s := ds.CastAI
	if s == nil {
		return dsSection{}, false
	}

	items := make([]dsIssue, 0, len(s.Recommendations))
	for _, rec := range s.Recommendations {
		items = append(items, dsIssue{health: rec.Health, key: nsName(rec.Namespace, rec.Name), detail: details(strings.Join(rec.Reasons, ", "), rec.Message)})
	}

	sec := newDSSection("CAST AI", "recommendations", details(s.Error, s.PlansError), items)

	for _, n := range s.Stuck {
		sec.issues = append(sec.issues, dsIssue{health: healthWarning, key: "node " + n.Node, detail: fmt.Sprintf("%d failed removals", n.Failures)})
	}

	return sec, true
}
