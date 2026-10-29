import BackgroundTasks
import Foundation
import IchorCore
import UIKit
import UserNotifications
import WidgetKit

enum BackgroundMonitor {
    static let taskID = "name.levis.ichor.refresh"
    static let alertsKey = "monitor.alerts"
    /// The clusters turned off with "Watch in the background", by context fingerprint (AppModel).
    static let unwatchedKey = "monitorUnwatchedClusters"

    static let unreachableKey = "monitor.unreachable"
    /// Opt-in: an alert when a cluster did not answer `unreachableRuns` runs in a row.
    static var unreachableWatched: Bool {
        get { UserDefaults.standard.bool(forKey: unreachableKey) }
        set { UserDefaults.standard.set(newValue, forKey: unreachableKey) }
    }

    static let unreachableRunsKey = "monitor.unreachableRuns"
    static var unreachableRuns: Int {
        get {
            let stored = UserDefaults.standard.integer(forKey: unreachableRunsKey)
            return unreachableRunsRange.contains(stored) ? stored : unreachableRunsDefault
        }
        set { UserDefaults.standard.set(newValue, forKey: unreachableRunsKey) }
    }

    /// The runs each cluster did not answer in a row (by monitor key), in the App Group.
    private static let reachabilityKey = "monitor.reachability"

    static func reachability() -> [String: Reachability] {
        SharedStore.defaults.data(forKey: reachabilityKey)
            .flatMap { try? JSONDecoder().decode([String: Reachability].self, from: $0) } ?? [:]
    }

    static func saveReachability(_ map: [String: Reachability]) {
        if !map.isEmpty, let data = try? JSONEncoder().encode(map) {
            SharedStore.defaults.set(data, forKey: reachabilityKey)
        } else {
            SharedStore.defaults.removeObject(forKey: reachabilityKey)
        }
    }

    static var alertsEnabled: Bool {
        get { UserDefaults.standard.bool(forKey: alertsKey) }
        set { UserDefaults.standard.set(newValue, forKey: alertsKey) }
    }

    static let dataServicesKey = "monitor.dataServices"
    /// Opt-in: also check Longhorn, Garage and CloudNativePG through the Kubernetes API.
    static var dataServicesWatched: Bool {
        get { UserDefaults.standard.bool(forKey: dataServicesKey) }
        set { UserDefaults.standard.set(newValue, forKey: dataServicesKey) }
    }

    static let importedPluginsKey = "monitor.importedPlugins"
    /// Opt-in: imported plugins' findings notify too. Off, only the built-in plugins' do: a plugin
    /// pasted in must not page anyone by itself.
    static var importedPluginAlerts: Bool {
        get { UserDefaults.standard.bool(forKey: importedPluginsKey) }
        set { UserDefaults.standard.set(newValue, forKey: importedPluginsKey) }
    }

    static let gitopsKey = "monitor.gitops"
    /// Opt-in: also check Argo CD and Flux apps through the Kubernetes API.
    static var gitopsWatched: Bool {
        get { UserDefaults.standard.bool(forKey: gitopsKey) }
        set { UserDefaults.standard.set(newValue, forKey: gitopsKey) }
    }

    static let checkupKey = "monitor.checkup"
    /// Opt-in: also run the cluster checkup through the Kubernetes API.
    static var checkupWatched: Bool {
        get { UserDefaults.standard.bool(forKey: checkupKey) }
        set { UserDefaults.standard.set(newValue, forKey: checkupKey) }
    }

    static let alertmanagerKey = "monitor.alertmanager"
    /// Opt-in: also read the Alertmanager's alerts (the cluster's source, else the first one found).
    static var alertmanagerWatched: Bool {
        get { UserDefaults.standard.bool(forKey: alertmanagerKey) }
        set { UserDefaults.standard.set(newValue, forKey: alertmanagerKey) }
    }

    static let storageKey = "monitor.storage"
    /// Opt-in: also read every node's volume fill and disks' SMART verdict (Talos clusters only).
    static var storageWatched: Bool {
        get { UserDefaults.standard.bool(forKey: storageKey) }
        set { UserDefaults.standard.set(newValue, forKey: storageKey) }
    }

    static let storageTrendKey = "monitor.storageTrend"
    /// Opt-in, under the storage watch: an early alert when a volume's growth over the last week
    /// (the history ring) makes it critical within days (CYR-128).
    static var storageTrendWatched: Bool {
        get { UserDefaults.standard.bool(forKey: storageTrendKey) }
        set { UserDefaults.standard.set(newValue, forKey: storageTrendKey) }
    }

    static let sensorsKey = "monitor.sensors"
    /// Opt-in: also read every node's hottest sensor, to alert when one runs hot or is throttled.
    static var sensorsWatched: Bool {
        get { UserDefaults.standard.bool(forKey: sensorsKey) }
        set { UserDefaults.standard.set(newValue, forKey: sensorsKey) }
    }

    static let sensorThresholdKey = "monitor.sensorThreshold"
    /// sensorThresholdAuto (each sensor's own limit) or the °C every node is held to.
    static var sensorThreshold: Int {
        get {
            let value = UserDefaults.standard.integer(forKey: sensorThresholdKey)
            return value == sensorThresholdAuto
                ? value : min(max(value, sensorThresholdRange.lowerBound), sensorThresholdRange.upperBound)
        }
        set { UserDefaults.standard.set(newValue, forKey: sensorThresholdKey) }
    }

    static let storageWarnKey = "monitor.storageWarn"
    static let storageCritKey = "monitor.storageCrit"
    /// The fill (% used) a volume alerts from: a warning, then critical (StorageThresholds clamps them).
    static var storageThresholds: StorageThresholds {
        get {
            let stored = UserDefaults.standard
            return StorageThresholds(
                warn: stored.object(forKey: storageWarnKey) as? Int ?? storageWarnDefault,
                crit: stored.object(forKey: storageCritKey) as? Int ?? storageCritDefault)
        }
        set {
            UserDefaults.standard.set(newValue.warn, forKey: storageWarnKey)
            UserDefaults.standard.set(newValue.crit, forKey: storageCritKey)
        }
    }

    /// Call once, before the app finishes launching.
    static func register() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: taskID, using: nil) { task in
            guard let task = task as? BGAppRefreshTask else { return }
            schedule()
            let work = Task {
                await check()
                task.setTaskCompleted(success: true)
            }
            task.expirationHandler = { work.cancel() }
        }
    }

    static func schedule() {
        let request = BGAppRefreshTaskRequest(identifier: taskID)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60)
        try? BGTaskScheduler.shared.submit(request)
    }

    /// One cluster to check, with what that takes.
    // Internal, not private: BackgroundMonitor+Alerts.swift reads it through ClusterAlerts.
    struct ClusterJob: Sendable {
        let context: ContextSummary
        /// Its monitor key (monitorClusterKey): what its snapshot and unreachable count are kept under.
        let key: String
        let client: TalosClient
        /// Its role may use the Kubernetes API (the opt-in parts need it).
        let kubeAllowed: Bool
        let previous: ClusterSnapshot?
    }

    /// What a run learnt of one cluster.
    private enum ClusterRead: Sendable {
        /// Read: its fresh snapshot (where no node may have answered: unreachable then too), with
        /// what the history ring keeps beyond it (versions, memory, volumes).
        case read(ClusterSnapshot, HistoryExtras)
        /// No answer, or none in time.
        case unreachable
        /// Not tried to the end (iOS ended the run): nothing changes for it.
        case skipped
    }

    /// What one cluster's history record is built from (see recordHistory).
    private struct HistoryInput {
        let key: String
        /// The fresh snapshot, nil when the cluster did not answer.
        let current: ClusterSnapshot?
        /// After evaluate: the issues notified (or kept from the last run for a track not read).
        let evaluated: ClusterSnapshot?
        let extras: HistoryExtras
    }

    /// Node storage as a run read it: the issues worth an alert (nil: not readable, the last
    /// snapshot's stand), and every volume's fill.
    private struct StorageRead: Sendable {
        let issues: [String: String]?
        let volumes: [HistoryVolumeRecord]
    }

    /// The alerts of one cluster, with the snapshots they are worded from.
    // Internal, not private: BackgroundMonitor+Alerts.swift posts them.
    struct ClusterAlerts {
        let job: ClusterJob
        let alerts: [Alert]
        let snapshot: ClusterSnapshot
        let previous: ClusterSnapshot?
    }

    /// One run: every cluster not turned off (one context each, see monitoredContexts), at most
    /// monitorParallel at a time within the refresh budget, each diffed with its own previous
    /// snapshot. Silent on a cluster unreachable as a whole (e.g. off VPN), past the opt-in
    /// "unreachable" alert after a few runs in a row.
    static func check() async {
        TalosClient.applyStoredPrivacyMask()  // also set at launch; keeps this path self-contained
        TalosClient.applyStoredAuditEvents()
        // Both stores, as the app lists them: the saved position counts kubeconfig clusters too.
        let stored = StoredConfigs.load()
        let parsed = await stored.parsed()
        guard parsed.unreadable.isEmpty, let summary = ConfigSummary.combined(talos: parsed.talos, kube: parsed.kube)
        else { return }
        let activeName = summary.selectedContext(
            index: AppModel.savedContextIndex, name: UserDefaults.standard.string(forKey: "activeContext"))
        // A cluster removed since drops its snapshot, unreachable count and snoozes (the app does too).
        let clusterKeys = summary.contexts.map(monitorClusterKey)
        AlertSnoozeStore.keep(clusters: summary.contexts.map(\.fingerprint))
        HistoryStore.keep(clusters: clusterKeys)
        var snapshots = keepingClusters(
            SharedStore.snapshots(activeCluster: summary.context(named: activeName).map(monitorClusterKey)),
            clusters: clusterKeys)
        let unwatched = Set(UserDefaults.standard.stringArray(forKey: unwatchedKey) ?? [])
        let contexts = monitoredContexts(summary.contexts, active: activeName, unwatched: unwatched)
        // A Talos cluster without an endpoint yet cannot be checked: the widget stops showing what
        // it showed (Android's clearSnapshot).
        for context in contexts where context.needsEndpoint { snapshots[monitorClusterKey(context)] = nil }
        // Renewed first, so the reads use fresh tokens and an idle-timeout session is kept alive;
        // nil on a locked device (the sign-ins cannot be read), when nothing is told either.
        let signIns = await TalosClient.renewStoredSignIns()
        // Read in monitorOrder (CYR-190): the clusters that missed the last run (a miss counted in
        // the reachability map) first; with the app in front ("Check now"), the cluster on screen last.
        let lastFailed = Set(reachability().filter { $0.value.misses > 0 }.keys)
        let appActive = await MainActor.run { UIApplication.shared.applicationState == .active }
        let ordered = monitorOrder(
            contexts.filter { !$0.needsEndpoint }, active: activeName, appActive: appActive,
            lastFailed: lastFailed)
        let jobs = await checkJobs(
            for: ordered, stored: stored,
            kubeContexts: parsed.kube?.contexts ?? [], snapshots: snapshots)
        let reads = await read(jobs)
        let now = Date()
        var reach = keepingClusters(reachability(), clusters: clusterKeys)
        let runs = unreachableRuns
        var outcomes: [ClusterAlerts] = []
        var history: [HistoryInput] = []
        for job in jobs {
            guard let result = reads[job.key] else { continue }
            var alerts: [Alert] = []
            var latest = job.previous
            let reachable: Bool
            switch result {
            case .skipped:
                continue
            case .unreachable:
                reachable = false
                history.append(HistoryInput(key: job.key, current: nil, evaluated: nil, extras: HistoryExtras()))
            case .read(let current, let extras):
                let evaluated = evaluate(previous: job.previous, current: current, now: now)
                snapshots[job.key] = evaluated.next
                latest = evaluated.next
                alerts = evaluated.alerts
                reachable = !current.unreachableAsAWhole
                history.append(HistoryInput(key: job.key, current: current, evaluated: evaluated.next, extras: extras))
            }
            // Counted even with the alert off, so turning it on knows the runs already missed.
            let counted = evaluateReachability(
                previous: reach[job.key], reachable: reachable, runs: runs,
                enabled: alertsEnabled && unreachableWatched)
            reach[job.key] = counted.next == Reachability() ? nil : counted.next
            if let alert = counted.alert { alerts.insert(alert, at: 0) }
            guard !alerts.isEmpty else { continue }
            let snapshot =
                latest ?? ClusterSnapshot(context: job.context.name, takenAt: now, nodes: [:], kube: job.context.isKube)
            outcomes.append(ClusterAlerts(job: job, alerts: alerts, snapshot: snapshot, previous: job.previous))
        }
        saveReachability(reach)
        // Real names only: under the screenshot mode the run read masked ones.
        if historyRecordingAllowed(privacyMasked: UserDefaults.standard.bool(forKey: PrivacyKeys.enabled)) {
            await recordHistory(history, at: now)
            // The storage trends come from the ring with this run's record in it.
            outcomes += await evaluateTrends(history, jobs: jobs, snapshots: &snapshots, now: now)
        }
        SharedStore.save(snapshots)
        // The pull requests opened from the phone: their states are kept even with alerts off,
        // so turning them on does not tell old merges.
        let proposals = await checkProposals(contexts: contexts.filter { !$0.needsEndpoint }, now: now)
        guard alertsEnabled else { return }
        await post(outcomes, now: now)
        await postProposals(proposals, now: now)
        if let signIns { await postSignIns(signIns, contexts: contexts, now: now) }
    }

    /// One record per cluster read (or not answering) in each one's sealed ring. A track not read
    /// this run resends the alerts the ring has open for it; a ring that cannot be read now (the
    /// device locked) or that a newer version wrote is left as it is.
    private static func recordHistory(_ inputs: [HistoryInput], at now: Date) async {
        let millis = Int64(now.timeIntervalSince1970 * 1000)
        for input in inputs {
            let stored = HistoryStore.ring(cluster: input.key)
            if stored == .unreadable { continue }
            let previous = await HistoryStore.openAlerts(in: stored, now: millis)
            let record = historyRecord(
                at: now, current: input.current, evaluated: input.evaluated, extras: input.extras,
                previousAlerts: previous, now: now)
            HistoryStore.append(record, cluster: input.key, stored: stored)
        }
    }

    /// The storage trend alerts (CYR-128) of each cluster whose storage was read this run, from
    /// its ring as just written: the ones open go in its snapshot. A cluster not read, or whose
    /// ring cannot be read, keeps its open ones; with the warning off they are forgotten.
    private static func evaluateTrends(
        _ inputs: [HistoryInput], jobs: [ClusterJob], snapshots: inout [String: ClusterSnapshot],
        now: Date
    ) async -> [ClusterAlerts] {
        let watched = storageTrendWatched
        var out: [ClusterAlerts] = []
        for input in inputs {
            guard let current = input.current, var snapshot = snapshots[input.key], snapshot.storageWatched else {
                continue
            }
            guard watched else {
                snapshot.storageTrends = [:]
                snapshots[input.key] = snapshot
                continue
            }
            guard current.storageChecked, let job = jobs.first(where: { $0.key == input.key }),
                let forecast = await HistoryStore.forecast(cluster: input.key, now: now)
            else { continue }
            // At or above the warning threshold, read now or still notified: the fill alert speaks for it.
            let fill = current.storageIssues.merging(snapshot.storageIssues) { fresh, _ in fresh }
            let outcome = evaluateStorageTrends(
                forecast, open: snapshot.storageTrends, fillIssues: fill,
                hostnames: snapshot.nodes.mapValues(\.hostname))
            snapshot.storageTrends = outcome.open
            snapshots[input.key] = snapshot
            if !outcome.alerts.isEmpty {
                out.append(ClusterAlerts(job: job, alerts: outcome.alerts, snapshot: snapshot, previous: job.previous))
            }
        }
        return out
    }

    /// The clusters of `contexts` to check now: a VPN-only one waits for its VPN (without it the
    /// check could only time out), and a cluster whose config is not stored is left out.
    private static func checkJobs(
        for contexts: [ContextSummary], stored: StoredConfigs, kubeContexts: [ContextSummary],
        snapshots: [String: ClusterSnapshot]
    ) async -> [ClusterJob] {
        let vpnOnly = Set(UserDefaults.standard.stringArray(forKey: "vpnOnlyClusters") ?? [])
        var vpnUp = true
        if contexts.contains(where: { vpnOnly.contains($0.fingerprint) }) { vpnUp = await VpnMonitor.currentlyUp() }
        // The Kubernetes API address the user set for each cluster, as the app uses it.
        let servers = UserDefaults.standard.dictionary(forKey: "kubeServers") as? [String: String] ?? [:]
        // And its Kubernetes access: a linked kubeconfig cluster (K5). A sign-in it needs is never
        // started from here: its calls fail and the app asks the user to sign in.
        let links = UserDefaults.standard.dictionary(forKey: "kubeAccess") as? [String: String] ?? [:]
        return contexts.compactMap { context -> ClusterJob? in
            guard !heldBackForVpn(vpnOnly: vpnOnly, fingerprint: context.fingerprint, vpnUp: vpnUp),
                let yaml = stored.config(for: context)
            else { return nil }
            let link =
                context.isKube
                ? nil
                : stored.kube.flatMap { kube in
                    kubeAccessContext(of: context, links: links, kubeContexts: kubeContexts).map {
                        KubeLink(config: kube, context: $0)
                    }
                }
            let client = TalosClient(
                config: yaml, context: context.name, kubeServer: servers[context.fingerprint] ?? "", kubeLink: link)
            let key = monitorClusterKey(context)
            return ClusterJob(
                context: context, key: key, client: client,
                kubeAllowed: context.allows(.workloads, kubeLinked: link != nil), previous: snapshots[key])
        }
    }

    /// Reads `jobs`, at most monitorParallel at a time, each within monitorClusterTimeout. Once
    /// iOS ends the run (the task cancelled) no other one starts and those under way are skipped.
    private static func read(_ jobs: [ClusterJob]) async -> [String: ClusterRead] {
        let timeout = monitorClusterTimeout(clusters: jobs.count)
        return await withTaskGroup(of: (String, ClusterRead).self) { group in
            var reads: [String: ClusterRead] = [:]
            var waiting = jobs[...]
            for job in waiting.prefix(monitorParallel) {
                group.addTask { (job.key, await read(job, timeout: timeout)) }
            }
            waiting = waiting.dropFirst(monitorParallel)
            while let done = await group.next() {
                reads[done.0] = done.1
                if let job = waiting.first, !Task.isCancelled {
                    waiting = waiting.dropFirst()
                    group.addTask { (job.key, await read(job, timeout: timeout)) }
                }
            }
            return reads
        }
    }

    /// One cluster: the Talos overview and etcd (a cluster added from a kubeconfig: its Kubernetes
    /// node list) within `timeout`, then the opt-in parts in what is left of it, side by side. A
    /// part not read in time keeps what the last snapshot knew of it, as one that failed.
    private static func read(_ job: ClusterJob, timeout: TimeInterval) async -> ClusterRead {
        let deadline = Date().addingTimeInterval(timeout)
        let client = job.client
        let context = job.context
        // Unreadable (off VPN, a sign-in needed, too slow): nothing to compare, the previous snapshot stays.
        var overview: ClusterOverview?
        var kubeNodes: KubeNodesOverview?
        if context.isKube {
            kubeNodes = await withDeadline(max(0, deadline.timeIntervalSinceNow)) { try? await client.kubeNodes() }
        } else {
            overview = await withDeadline(max(0, deadline.timeIntervalSinceNow)) { try? await client.overview() }
        }
        guard overview != nil || kubeNodes != nil else { return Task.isCancelled ? .skipped : .unreachable }
        var etcd: EtcdOverview?
        if !context.isKube {
            etcd = await withDeadline(max(0, deadline.timeIntervalSinceNow)) { try? await client.etcd() }
        }
        let left = max(0, deadline.timeIntervalSinceNow)
        let previous = job.previous
        // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
        // resources and runs the Garage CLI in a pod.
        let watchData = dataServicesWatched && job.kubeAllowed
        // With them, the CRD plugins' findings (KubePluginFindings): one bounded call for every
        // plugin the cluster runs.
        let watchPlugins = watchData
        let knownPlugins = knownPluginIssues(previous, context: context.name)
        // Same gate for Argo CD and Flux apps, read through their custom resources.
        let watchGitOps = gitopsWatched && job.kubeAllowed
        let known = knownGitOpsIssues(previous, context: context.name)
        // And for the checkup: it lists the cluster's pods and asks every kubelet.
        let watchCheckup = checkupWatched && job.kubeAllowed
        let knownCheckup = knownCheckupIssues(previous, context: context.name)
        // And for the Alertmanager, reached like the metrics through the service proxy or a URL.
        let watchAlertmanager = alertmanagerWatched && job.kubeAllowed
        let knownAlertmanager = knownAlertmanagerIssues(previous, context: context.name)
        // And node storage: through the Talos API (any role may read it), or on a kubeconfig cluster
        // each kubelet's summary (credentials refused nodes/proxy: no reading, never an alert). A
        // node still listed that did not answer keeps its issues. The demo's disks are read (its
        // history shows them) but never open an issue: a demo never notifies.
        let watchStorage = storageWatched
        let kubeListed = kubeNodes.map { Set($0.nodes.map(\.name)) }
        let demo = context.demo
        let knownStorage = knownStorageIssues(previous, context: context.name)
        let thresholds = storageThresholds
        // And node temperatures, the same way: one Talos call, nothing read with the setting off.
        let watchSensors = sensorsWatched && !context.isKube
        let knownSensors = knownSensorIssues(previous, context: context.name)
        let sensorLimit = sensorThreshold
        let fingerprint = context.fingerprint
        async let dataRead = ifWatched(watchData, within: left) { try? await client.dataServices(hints: "") }
        async let gitopsRead = ifWatched(watchGitOps, within: left) { await readGitOps(client, known: known) }
        async let checkupRead = ifWatched(watchCheckup, within: left) {
            await readCheckup(
                client, known: knownCheckup, gitopsWatched: watchGitOps, pluginsWatched: watchPlugins,
                importedWatched: importedPluginAlerts)
        }
        async let pluginRead = ifWatched(watchPlugins, within: left) {
            await readPlugins(
                client, known: knownPlugins, checkupWatched: watchCheckup, importedWatched: importedPluginAlerts)
        }
        async let alertmanagerRead = ifWatched(watchAlertmanager, within: left) {
            await readAlertmanager(client, fingerprint: fingerprint, known: knownAlertmanager)
        }
        async let storageRead = ifWatched(watchStorage, within: left) { () -> StorageRead? in
            if let kubeListed {
                return await readKubeStorage(
                    client, thresholds: thresholds, listed: kubeListed, known: knownStorage, demo: demo)
            }
            return await readStorage(client, thresholds: thresholds, known: knownStorage)
        }
        async let sensorRead = ifWatched(watchSensors, within: left) { () -> [String: String]? in
            guard let health = try? await client.clusterSensorHealth() else { return nil }
            return sensorIssuesOf(health, threshold: sensorLimit, known: knownSensors)
        }
        let dataServices = await dataRead
        let gitopsIssues = await gitopsRead
        let checkupIssues = await checkupRead
        let alertmanagerIssues = await alertmanagerRead
        let storage = await storageRead
        let storageIssues = storage?.issues
        let sensorIssues = await sensorRead
        let pluginIssues = await pluginRead
        let now = Date()
        if let kubeNodes {
            var kubeExtras = historyExtras(kubeNodes)
            kubeExtras.volumes = storage?.volumes ?? []
            return .read(
                kubeSnapshotOf(
                    kubeNodes, context: context.name, certNotAfter: context.certNotAfter, takenAt: now,
                    dataWatched: watchData, dataServices: dataServices,
                    gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues,
                    checkupWatched: watchCheckup, checkupIssues: checkupIssues,
                    alertmanagerWatched: watchAlertmanager, alertmanagerIssues: alertmanagerIssues,
                    storageWatched: watchStorage, storageIssues: storageIssues, storageWarn: thresholds.warn,
                    pluginWatched: watchPlugins, pluginIssues: pluginIssues),
                kubeExtras)
        }
        guard let overview else { return .unreachable }
        var extras = historyExtras(overview)
        extras.volumes = storage?.volumes ?? []
        return .read(
            snapshotOf(
                overview, etcd: etcd, certNotAfter: context.certNotAfter, takenAt: now,
                dataWatched: watchData, dataServices: dataServices,
                gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues,
                checkupWatched: watchCheckup, checkupIssues: checkupIssues,
                alertmanagerWatched: watchAlertmanager, alertmanagerIssues: alertmanagerIssues,
                storageWatched: watchStorage, storageIssues: storageIssues, storageWarn: thresholds.warn,
                sensorWatched: watchSensors, sensorIssues: sensorIssues,
                pluginWatched: watchPlugins, pluginIssues: pluginIssues),
            extras)
    }

    /// `work` within `seconds` when `on`; nil when off, failed or too slow.
    private static func ifWatched<T: Sendable>(
        _ on: Bool, within seconds: TimeInterval,
        _ work: @escaping @Sendable () async -> T?
    ) async -> T? {
        guard on else { return nil }
        return await withDeadline(seconds, work)
    }

    /// could be read. A part that could not be read keeps what the last snapshot knew of it.
    private static func readGitOps(_ client: TalosClient, known: [String: String]) async -> [String: String]? {
        async let argo = try? client.argoCD()
        async let flux = try? client.flux()
        return gitopsIssuesWithGaps(argo: await argo, flux: await flux, known: known)
    }

    /// The checkup's findings worth an alert; nil when it could not be read. A section that could
    /// not be read keeps what the last snapshot knew of it. With GitOps watched, what that alert
    /// already raises is left to it; with the plugins watched, their findings to their own alert.
    private static func readCheckup(
        _ client: TalosClient, known: [String: String], gitopsWatched: Bool,
        pluginsWatched: Bool, importedWatched: Bool
    ) async -> [String: String]? {
        guard let report = try? await client.checkup() else { return nil }
        return checkupIssuesWithGaps(
            report, known: known, gitopsWatched: gitopsWatched, pluginsWatched: pluginsWatched,
            importedPluginsWatched: importedWatched)
    }

    /// The CRD plugins' findings worth an alert, in the app's language; nil when the call failed.
    /// A plugin that could not be read keeps what the last snapshot knew of it. With the checkup
    /// watched, the findings it reports from a section of its own are left to it.
    private static func readPlugins(
        _ client: TalosClient, known: [String: String], checkupWatched: Bool,
        importedWatched: Bool
    ) async -> [String: String]? {
        guard let report = try? await client.pluginFindings() else { return nil }
        return pluginIssuesWithGaps(
            report, known: known, checkupWatched: checkupWatched, importedWatched: importedWatched)
    }

    /// The Alertmanager's alerts worth a notification; nil when it could not be read (or none was
    /// found): the last snapshot's then stand. Past 1000 alerts, a known one missing from the
    /// answer is kept rather than reported resolved.
    private static func readAlertmanager(_ client: TalosClient, fingerprint: String, known: [String: String]) async
        -> [String: String]?
    {
        guard let source = try? await AlertmanagerStore.resolve(fingerprint, with: client),
            let alerts = try? await client.alertmanagerAlerts(source)
        else { return nil }
        return alertmanagerIssuesWithGaps(alerts, known: known)
    }

    /// Volumes over `thresholds` and disks failing SMART, with every volume's fill (the history);
    /// nil when it could not be read. A node that did not answer keeps what the last snapshot knew of it.
    private static func readStorage(
        _ client: TalosClient, thresholds: StorageThresholds,
        known: [String: String]
    ) async -> StorageRead? {
        guard let health = try? await client.clusterStorageHealth() else { return nil }
        return StorageRead(
            issues: storageIssuesOf(health, thresholds: thresholds, known: known), volumes: historyVolumes(health))
    }

    /// A kubeconfig cluster's nodes over `thresholds` or under DiskPressure (see
    /// storageIssues(of:warn:critical:listed:known:demo:)), with each measured node's nodefs fill
    /// (the history); nil when the call failed. Issues nil when skipped, refused or nothing measured.
    private static func readKubeStorage(
        _ client: TalosClient, thresholds: StorageThresholds, listed: Set<String>,
        known: [String: String], demo: Bool
    ) async -> StorageRead? {
        guard let disk = try? await client.kubeNodeDisk() else { return nil }
        return StorageRead(
            issues: storageIssues(
                of: disk, warn: thresholds.warn, critical: thresholds.crit,
                listed: listed, known: known, demo: demo),
            volumes: historyVolumes(disk))
    }
}
