package ichorgo

import (
	"context"
	"sync"
	"time"
)

// The ReadProgress op of Data services and its steps: the API groups, the listing of every
// pod (when Garage or ExternalDNS is looked for), then one step per system detected, named by
// a fixed id (never an object name).
const (
	dsProgressOp = "dataservices"

	dsStepAPI           = "api"
	dsStepPods          = "pods"
	dsStepSnapshots     = "snapshots"
	dsStepBackups       = "backups"
	dsStepLonghorn      = "longhorn"
	dsStepCNPG          = "cnpg"
	dsStepDragonfly     = "dragonfly"
	dsStepMariaDB       = "mariadb"
	dsStepPercona       = "percona"
	dsStepCertManager   = "certmanager"
	dsStepSealedSecrets = "sealedsecrets"
	dsStepVelero        = "velero"
	dsStepExternalDNS   = "externaldns"
	dsStepKubeGreen     = "kubegreen"
	dsStepCeph          = "ceph"
	dsStepCastAI        = "castai"
	dsStepGarage        = "garage"
)

// dsDemoSteps is the short sequence the demo context plays.
var dsDemoSteps = []string{dsStepAPI, dsStepLonghorn, dsStepCNPG, dsStepGarage}

// dsInput is what the readers of Data services share: the detection, then the pod listing.
type dsInput struct {
	groups map[string]string
	hints  hintSet
	run    execFunc
	now    time.Time

	pods      []dsPod
	podsErr   error
	listedAll bool
}

// dsReader reads one system into out and tells whether it failed (its section's error).
type dsReader struct {
	id   string
	read func(out *dataServices) (failed bool)
}

// sectionFailed tells whether a section was read with an error.
func sectionFailed[T any](s *T, errOf func(*T) string) bool { return s != nil && errOf(s) != "" }

// readDataServices detects what is installed and reads each system in parallel. It fails
// only when nothing could be asked (the API groups); a system that fails on its own carries
// its error in its section. Its progress starts once the systems are detected, so its total
// is exact: a cluster without any has the API groups, the pods and the backup Jobs only.
func readDataServices(ctx context.Context, k *kubeClient, run execFunc, hints hintSet, now time.Time) (dataServices, error) {
	started := time.Now()

	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		progress := startReadProgressIn(ctx, dsStepAPI)
		defer progress.finish()

		progress.endAt(dsStepAPI, started, true)

		return dataServices{}, err
	}

	in := &dsInput{groups: groups, hints: hints, run: run, now: now}
	// One listing of every pod stands in for several: Garage's instances and, when the
	// DNSEndpoint CRD is not served, the ExternalDNS controllers (found by their pods).
	listPods := hints.wants("garage") || hints.wants("external-dns")
	readers := dataServiceReaders(ctx, k, in, listPods)

	steps := []string{dsStepAPI}
	if listPods {
		steps = append(steps, dsStepPods)
	}

	for _, r := range readers {
		steps = append(steps, r.id)
	}

	progress := startReadProgressIn(ctx, steps...)
	defer progress.finish()

	progress.endAt(dsStepAPI, started, false)

	if listPods {
		end := progress.track(dsStepPods)
		in.pods, in.podsErr = listDSPods(ctx, k, "")
		in.listedAll = in.podsErr == nil
		end(in.podsErr != nil)
	}

	var (
		out dataServices
		wg  sync.WaitGroup
	)

	for _, r := range readers {
		wg.Go(func() {
			end := progress.track(r.id)
			end(r.read(&out))
		})
	}

	wg.Wait()

	return out, nil
}

// dataServiceReaders is a reader per system the API groups show, in the order of their steps.
// Readers that need the pods read in.pods when they run, after the listing.
func dataServiceReaders(ctx context.Context, k *kubeClient, in *dsInput, listPods bool) []dsReader {
	groups, now := in.groups, in.now

	var readers []dsReader

	add := func(id string, ok bool, read func(out *dataServices) bool) {
		if ok {
			readers = append(readers, dsReader{id: id, read: read})
		}
	}
	served := func(group string) bool { _, ok := groups[group]; return ok }

	add(dsStepSnapshots, served(groupSnapshot), func(out *dataServices) bool {
		r := readVolumeSnapshots(ctx, k, "", now)
		out.Snapshots = &r

		return r.Error != ""
	})
	add(dsStepBackups, true, func(out *dataServices) bool {
		out.BackupJobs = readBackupJobs(ctx, k, now)

		return sectionFailed(out.BackupJobs, func(s *backupJobStatus) string { return s.Error })
	})
	add(dsStepLonghorn, served(groupLonghorn), func(out *dataServices) bool {
		out.Longhorn = readLonghorn(ctx, k, groups[groupLonghorn])

		return sectionFailed(out.Longhorn, func(s *longhornStatus) string { return s.Error })
	})
	add(dsStepCNPG, served(groupCNPG), func(out *dataServices) bool {
		out.CNPG = readCNPGSection(ctx, k, in)

		return sectionFailed(out.CNPG, func(s *cnpgStatus) string { return s.Error })
	})
	add(dsStepDragonfly, served(groupDragonfly), func(out *dataServices) bool {
		out.Dragonfly = readDragonfly(ctx, k, groups[groupDragonfly])

		return sectionFailed(out.Dragonfly, func(s *dragonflyStatus) string { return s.Error })
	})
	add(dsStepMariaDB, served(groupMariaDB), func(out *dataServices) bool {
		out.MariaDB = readMariaDB(ctx, k, groups[groupMariaDB], now)

		return sectionFailed(out.MariaDB, func(s *mariadbStatus) string { return s.Error })
	})
	add(dsStepPercona, served(groupPercona), func(out *dataServices) bool {
		out.Percona = readPercona(ctx, k, groups[groupPercona], now)

		return sectionFailed(out.Percona, func(s *perconaStatus) string { return s.Error })
	})
	add(dsStepCertManager, served(groupCertManager), func(out *dataServices) bool {
		out.CertManager = readCertManager(ctx, k, groups[groupCertManager], now)

		return sectionFailed(out.CertManager, func(s *certManagerStatus) string { return s.Error })
	})
	add(dsStepSealedSecrets, served(groupSealedSecrets), func(out *dataServices) bool {
		out.SealedSecrets = readSealedSecrets(ctx, k, now)

		return sectionFailed(out.SealedSecrets, func(s *sealedSecretsStatus) string { return s.Error })
	})
	add(dsStepVelero, served(groupVelero), func(out *dataServices) bool {
		out.Velero = readVelero(ctx, k, now)

		return sectionFailed(out.Velero, func(s *veleroStatus) string { return s.Error })
	})
	// ExternalDNS runs with or without its CRD: by the API group when served, else by its
	// running pods (only known once they are listed: the step ends at once without them).
	add(dsStepExternalDNS, served(groupExternalDNS) || listPods, func(out *dataServices) bool {
		out.ExternalDNS = readExternalDNSSection(ctx, k, in)

		return sectionFailed(out.ExternalDNS, func(s *externalDNSStatus) string { return s.Error })
	})
	add(dsStepKubeGreen, served(groupKubeGreen), func(out *dataServices) bool {
		out.KubeGreen = readKubeGreen(ctx, k, now)

		return sectionFailed(out.KubeGreen, func(s *kubeGreenStatus) string { return s.Error })
	})
	add(dsStepCeph, served(groupCeph), func(out *dataServices) bool {
		out.Ceph = readCeph(ctx, k, groups[groupCeph])

		return sectionFailed(out.Ceph, func(s *cephStatus) string { return s.Error })
	})
	add(dsStepCastAI, served(groupCastAI), func(out *dataServices) bool {
		out.CastAI = readCastAI(ctx, k)

		return sectionFailed(out.CastAI, func(s *castAIStatus) string { return s.Error })
	})
	// The garage-operator's CRDs mean Garage is managed here, hinted or not. Its exec can hang
	// to its own timeout: the step fails on the error it ends with, it never stays running.
	add(dsStepGarage, in.hints.wants("garage") || served(groupGarageOperator), func(out *dataServices) bool {
		out.Garage = readGarageSection(ctx, k, in.run, in.pods, in.podsErr, in.hints["garage"], groups[groupGarageOperator])

		return sectionFailed(out.Garage, func(s *garageStatus) string { return s.Error })
	})

	return readers
}

// readCNPGSection reads CloudNativePG with the pods listed for every system, or its own.
func readCNPGSection(ctx context.Context, k *kubeClient, in *dsInput) *cnpgStatus {
	pods, err := in.pods, error(nil)
	if !in.listedAll {
		pods, err = listDSPods(ctx, k, "cnpg.io/cluster")
	}

	src := cnpgSources{version: in.groups[groupCNPG], pluginVersion: in.groups[groupBarmanPlug], pods: pods, podsErr: err}

	return readCNPG(ctx, k, src, in.now)
}

// readExternalDNSSection reads ExternalDNS when its API group is served or its controllers
// run among the pods listed; nil when neither.
func readExternalDNSSection(ctx context.Context, k *kubeClient, in *dsInput) *externalDNSStatus {
	version := in.groups[groupExternalDNS]
	if _, served := in.groups[groupExternalDNS]; !served && (!in.listedAll || !runsExternalDNS(in.pods)) {
		return nil
	}

	return readExternalDNS(ctx, k, version, in.pods, in.podsErr, in.now)
}

// runsExternalDNS tells whether one of pods runs an ExternalDNS image.
func runsExternalDNS(pods []dsPod) bool {
	catalog := loadAppCatalog()

	for _, p := range pods {
		for _, c := range p.Spec.Containers {
			if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "external-dns" {
				return true
			}
		}
	}

	return false
}
