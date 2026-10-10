import BackgroundTasks
import Foundation
import IchorCore
import UserNotifications
import WidgetKit

/// Shared with the widget extension (App Group): a snapshot per cluster (by monitor key, see
/// monitorClusterKey), the clusters the widget can pick and the active one.
enum SharedStore {
    static var defaults: UserDefaults { UserDefaults(suiteName: SharedSnapshot.suite) ?? .standard }

    /// Every cluster's last snapshot; the single one of older versions goes to `activeCluster`.
    static func snapshots(activeCluster: String?) -> [String: ClusterSnapshot] {
        migratedSnapshots(SharedSnapshot.loadAll(from: defaults), legacy: SharedSnapshot.load(from: defaults), activeCluster: activeCluster)
    }

    /// Stores `snapshots` (the older versions' single one goes) and reloads the widget.
    static func save(_ snapshots: [String: ClusterSnapshot]) {
        if !snapshots.isEmpty, let data = try? JSONEncoder().encode(snapshots) {
            defaults.set(data, forKey: SharedSnapshot.snapshotsKey)
        } else {
            defaults.removeObject(forKey: SharedSnapshot.snapshotsKey)
        }
        defaults.removeObject(forKey: SharedSnapshot.key)
        WidgetCenter.shared.reloadAllTimelines()
    }

    /// Only the state of the clusters still stored (monitor keys): a removed cluster's snapshot
    /// and unreachable count go with it.
    static func keep(clusters: [String]) {
        let stored = SharedSnapshot.loadAll(from: defaults)
        let kept = keepingClusters(stored, clusters: clusters)
        if kept.count != stored.count { save(kept) }
        let reach = BackgroundMonitor.reachability()
        let keptReach = keepingClusters(reach, clusters: clusters)
        if keptReach.count != reach.count { BackgroundMonitor.saveReachability(keptReach) }
    }

    /// Forgets every snapshot (a config replaced, the screenshot mode toggled): the next run is a baseline.
    static func clear() {
        save([:])
        BackgroundMonitor.saveReachability([:])
    }

    /// The clusters the widget can pick, named as on screen, and the active one (its default).
    static func publish(clusters: [WidgetCluster], active: String?) {
        let data = clusters.isEmpty ? nil : (try? JSONEncoder().encode(clusters))
        guard data != defaults.data(forKey: SharedSnapshot.clustersKey) || active != SharedSnapshot.activeCluster(from: defaults) else { return }
        if let data { defaults.set(data, forKey: SharedSnapshot.clustersKey) } else { defaults.removeObject(forKey: SharedSnapshot.clustersKey) }
        if let active { defaults.set(active, forKey: SharedSnapshot.activeKey) } else { defaults.removeObject(forKey: SharedSnapshot.activeKey) }
        WidgetCenter.shared.reloadAllTimelines()
    }
}

/// Best-effort background checks: iOS decides when refresh tasks run (often every 15-60
/// minutes). The config can only be decrypted while the device is unlocked, so checks made
/// while it is locked are skipped (and with a security key required, once iOS closed the app).
/// Each run checks every cluster not turned off. Same alert rules as Android (IchorCore.evaluate).
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

    static let storageWarnKey = "monitor.storageWarn"
    static let storageCritKey = "monitor.storageCrit"
    /// The fill (% used) a volume alerts from: a warning, then critical (StorageThresholds clamps them).
    static var storageThresholds: StorageThresholds {
        get {
            let stored = UserDefaults.standard
            return StorageThresholds(warn: stored.object(forKey: storageWarnKey) as? Int ?? storageWarnDefault,
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
    private struct ClusterJob: Sendable {
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

    /// Node storage as a run read it: the issues worth an alert, and every volume's fill.
    private struct StorageRead: Sendable {
        let issues: [String: String]
        let volumes: [HistoryVolumeRecord]
    }

    /// The alerts of one cluster, with the snapshots they are worded from.
    private struct ClusterAlerts {
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
        TalosClient.applyStoredPrivacyMask() // also set at launch; keeps this path self-contained
        // Both stores, as the app lists them: the saved position counts kubeconfig clusters too.
        let stored = StoredConfigs.load()
        let parsed = await stored.parsed()
        guard parsed.unreadable.isEmpty, let summary = ConfigSummary.combined(talos: parsed.talos, kube: parsed.kube) else { return }
        let activeName = summary.selectedContext(index: AppModel.savedContextIndex, name: UserDefaults.standard.string(forKey: "activeContext"))
        // A cluster removed since drops its snapshot, unreachable count and snoozes (the app does too).
        let clusterKeys = summary.contexts.map(monitorClusterKey)
        AlertSnoozeStore.keep(clusters: summary.contexts.map(\.fingerprint))
        HistoryStore.keep(clusters: clusterKeys)
        var snapshots = keepingClusters(SharedStore.snapshots(activeCluster: summary.context(named: activeName).map(monitorClusterKey)),
                                        clusters: clusterKeys)
        let unwatched = Set(UserDefaults.standard.stringArray(forKey: unwatchedKey) ?? [])
        let contexts = monitoredContexts(summary.contexts, active: activeName, unwatched: unwatched)
        // A Talos cluster without an endpoint yet cannot be checked: the widget stops showing what
        // it showed (Android's clearSnapshot).
        for context in contexts where context.needsEndpoint { snapshots[monitorClusterKey(context)] = nil }
        let jobs = await checkJobs(for: contexts.filter { !$0.needsEndpoint }, stored: stored,
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
            let counted = evaluateReachability(previous: reach[job.key], reachable: reachable, runs: runs,
                                               enabled: alertsEnabled && unreachableWatched)
            reach[job.key] = counted.next == Reachability() ? nil : counted.next
            if let alert = counted.alert { alerts.insert(alert, at: 0) }
            guard !alerts.isEmpty else { continue }
            let snapshot = latest ?? ClusterSnapshot(context: job.context.name, takenAt: now, nodes: [:], kube: job.context.isKube)
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
        guard alertsEnabled else { return }
        await post(outcomes, now: now)
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
            let record = historyRecord(at: now, current: input.current, evaluated: input.evaluated, extras: input.extras,
                                       previousAlerts: previous, now: now)
            HistoryStore.append(record, cluster: input.key, stored: stored)
        }
    }

    /// The storage trend alerts (CYR-128) of each cluster whose storage was read this run, from
    /// its ring as just written: the ones open go in its snapshot. A cluster not read, or whose
    /// ring cannot be read, keeps its open ones; with the warning off they are forgotten.
    private static func evaluateTrends(_ inputs: [HistoryInput], jobs: [ClusterJob], snapshots: inout [String: ClusterSnapshot],
                                       now: Date) async -> [ClusterAlerts] {
        let watched = storageTrendWatched
        var out: [ClusterAlerts] = []
        for input in inputs {
            guard let current = input.current, var snapshot = snapshots[input.key], snapshot.storageWatched else { continue }
            guard watched else {
                snapshot.storageTrends = [:]
                snapshots[input.key] = snapshot
                continue
            }
            guard current.storageChecked, let job = jobs.first(where: { $0.key == input.key }),
                  let forecast = await HistoryStore.forecast(cluster: input.key, now: now) else { continue }
            // At or above the warning threshold, read now or still notified: the fill alert speaks for it.
            let fill = current.storageIssues.merging(snapshot.storageIssues) { fresh, _ in fresh }
            let outcome = evaluateStorageTrends(forecast, open: snapshot.storageTrends, fillIssues: fill,
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
    private static func checkJobs(for contexts: [ContextSummary], stored: StoredConfigs, kubeContexts: [ContextSummary],
                                  snapshots: [String: ClusterSnapshot]) async -> [ClusterJob] {
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
                  let yaml = stored.config(for: context) else { return nil }
            let link = context.isKube ? nil : stored.kube.flatMap { kube in
                kubeAccessContext(of: context, links: links, kubeContexts: kubeContexts).map { KubeLink(config: kube, context: $0) }
            }
            let client = TalosClient(config: yaml, context: context.name, kubeServer: servers[context.fingerprint] ?? "", kubeLink: link)
            let key = monitorClusterKey(context)
            return ClusterJob(context: context, key: key, client: client,
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
        if !context.isKube { etcd = await withDeadline(max(0, deadline.timeIntervalSinceNow)) { try? await client.etcd() } }
        let left = max(0, deadline.timeIntervalSinceNow)
        let previous = job.previous
        // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
        // resources and runs the Garage CLI in a pod.
        let watchData = dataServicesWatched && job.kubeAllowed
        // Same gate for Argo CD and Flux apps, read through their custom resources.
        let watchGitOps = gitopsWatched && job.kubeAllowed
        let known = knownGitOpsIssues(previous, context: context.name)
        // And for the checkup: it lists the cluster's pods and asks every kubelet.
        let watchCheckup = checkupWatched && job.kubeAllowed
        let knownCheckup = knownCheckupIssues(previous, context: context.name)
        // And for the Alertmanager, reached like the metrics through the service proxy or a URL.
        let watchAlertmanager = alertmanagerWatched && job.kubeAllowed
        let knownAlertmanager = knownAlertmanagerIssues(previous, context: context.name)
        // And node storage, through the Talos API only (any role may read it): none on a kubeconfig cluster.
        let watchStorage = storageWatched && !context.isKube
        let knownStorage = knownStorageIssues(previous, context: context.name)
        let thresholds = storageThresholds
        let fingerprint = context.fingerprint
        async let dataRead = ifWatched(watchData, within: left) { try? await client.dataServices(hints: "") }
        async let gitopsRead = ifWatched(watchGitOps, within: left) { await readGitOps(client, known: known) }
        async let checkupRead = ifWatched(watchCheckup, within: left) { await readCheckup(client, known: knownCheckup) }
        async let alertmanagerRead = ifWatched(watchAlertmanager, within: left) {
            await readAlertmanager(client, fingerprint: fingerprint, known: knownAlertmanager)
        }
        async let storageRead = ifWatched(watchStorage, within: left) {
            await readStorage(client, thresholds: thresholds, known: knownStorage)
        }
        let dataServices = await dataRead
        let gitopsIssues = await gitopsRead
        let checkupIssues = await checkupRead
        let alertmanagerIssues = await alertmanagerRead
        let storage = await storageRead
        let storageIssues = storage?.issues
        let now = Date()
        if let kubeNodes {
            return .read(kubeSnapshotOf(kubeNodes, context: context.name, certNotAfter: context.certNotAfter, takenAt: now,
                                        dataWatched: watchData, dataServices: dataServices,
                                        gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues,
                                        checkupWatched: watchCheckup, checkupIssues: checkupIssues,
                                        alertmanagerWatched: watchAlertmanager, alertmanagerIssues: alertmanagerIssues),
                         historyExtras(kubeNodes))
        }
        guard let overview else { return .unreachable }
        var extras = historyExtras(overview)
        extras.volumes = storage?.volumes ?? []
        return .read(snapshotOf(overview, etcd: etcd, certNotAfter: context.certNotAfter, takenAt: now,
                                dataWatched: watchData, dataServices: dataServices,
                                gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues,
                                checkupWatched: watchCheckup, checkupIssues: checkupIssues,
                                alertmanagerWatched: watchAlertmanager, alertmanagerIssues: alertmanagerIssues,
                                storageWatched: watchStorage, storageIssues: storageIssues, storageWarn: thresholds.warn),
                     extras)
    }

    /// `work` within `seconds` when `on`; nil when off, failed or too slow.
    private static func ifWatched<T: Sendable>(_ on: Bool, within seconds: TimeInterval,
                                               _ work: @escaping @Sendable () async -> T?) async -> T? {
        guard on else { return nil }
        return await withDeadline(seconds, work)
    }

    /// Posts each cluster's alerts, named after their cluster (the notification's subtitle).
    private static func post(_ outcomes: [ClusterAlerts], now: Date) async {
        let hide = UserDefaults.standard.bool(forKey: "appLockEnabled")
        let names = UserDefaults.standard.dictionary(forKey: "clusterNames") as? [String: String] ?? [:]
        let labels = ClusterLabels(names: names, masked: UserDefaults.standard.bool(forKey: PrivacyKeys.enabled))
        let snoozes = AlertSnoozeStore.current(now: now)
        for outcome in outcomes {
            let context = outcome.job.context
            let fingerprint = context.fingerprint
            // A snoozed alert posts nothing, neither the problem nor its end (its notification's Snooze).
            for alert in snoozes.notSnoozed(outcome.alerts, cluster: fingerprint, now: now) {
                let link = alertLink(alert, snapshot: outcome.snapshot, cluster: context.clusterID)
                let text = localized(alert, snapshot: outcome.snapshot, previous: outcome.previous, now: now)
                let wake = await canWake(alert, cluster: fingerprint)
                await post(alert, localized: text, cluster: labels.of(context),
                           id: alertNotificationID(cluster: outcome.job.key, alertKey: alert.key), link: link,
                           actions: alertActions(key: alert.key, problem: alert.problem, canWake: wake),
                           info: actionInfo(alert, snapshot: outcome.snapshot, cluster: fingerprint), hideDetails: hide)
            }
        }
    }

    /// Whether the node of a node alert can be woken: a Wake-on-LAN setting or a MAC seen.
    private static func canWake(_ alert: Alert, cluster: String) async -> Bool {
        guard alert.problem, !cluster.isEmpty, alert.key.hasPrefix("node:") else { return false }
        let address = String(alert.key.dropFirst("node:".count))
        return await MainActor.run {
            WakeOnLanStore.shared.loadIfNeeded()
            return !WakeOnLanStore.shared.wakeTargets(fingerprint: cluster, node: address).isEmpty
        }
    }

    /// What the notification's Snooze and Wake need: the cluster, the alert, a node's hostname.
    private static func actionInfo(_ alert: Alert, snapshot: ClusterSnapshot, cluster: String) -> [String: String] {
        var info = [AlertNotificationActions.clusterKey: cluster, AlertNotificationActions.alertKey: alert.key]
        if alert.key.hasPrefix("node:"), let state = snapshot.nodes[String(alert.key.dropFirst("node:".count))] {
            info[AlertNotificationActions.hostKey] = state.hostname
        }
        return info
    }

    /// Argo CD and Flux issues (a tool not installed reads fine and adds nothing); nil when neither
    /// could be read. A part that could not be read keeps what the last snapshot knew of it.
    private static func readGitOps(_ client: TalosClient, known: [String: String]) async -> [String: String]? {
        async let argo = try? client.argoCD()
        async let flux = try? client.flux()
        return gitopsIssuesWithGaps(argo: await argo, flux: await flux, known: known)
    }

    /// The checkup's findings worth an alert; nil when it could not be read. A section that could
    /// not be read keeps what the last snapshot knew of it.
    private static func readCheckup(_ client: TalosClient, known: [String: String]) async -> [String: String]? {
        guard let report = try? await client.checkup() else { return nil }
        return checkupIssuesWithGaps(report, known: known)
    }

    /// The Alertmanager's alerts worth a notification; nil when it could not be read (or none was
    /// found): the last snapshot's then stand. Past 1000 alerts, a known one missing from the
    /// answer is kept rather than reported resolved.
    private static func readAlertmanager(_ client: TalosClient, fingerprint: String, known: [String: String]) async -> [String: String]? {
        guard let source = try? await AlertmanagerStore.resolve(fingerprint, with: client),
              let alerts = try? await client.alertmanagerAlerts(source) else { return nil }
        return alertmanagerIssuesWithGaps(alerts, known: known)
    }

    /// Volumes over `thresholds` and disks failing SMART, with every volume's fill (the history);
    /// nil when it could not be read. A node that did not answer keeps what the last snapshot knew of it.
    private static func readStorage(_ client: TalosClient, thresholds: StorageThresholds,
                                    known: [String: String]) async -> StorageRead? {
        guard let health = try? await client.clusterStorageHealth() else { return nil }
        return StorageRead(issues: storageIssuesOf(health, thresholds: thresholds, known: known), volumes: historyVolumes(health))
    }

    /// IchorCore builds English alerts; this rebuilds their text in the user's
    /// language from the alert key and the snapshot (same wording, keys in Localizable.xcstrings).
    /// `previous`: the snapshot before the check, which still names an Alertmanager alert that resolved.
    static func localized(_ alert: Alert, snapshot: ClusterSnapshot, previous: ClusterSnapshot? = nil,
                          now: Date) -> (title: String, text: String) {
        let parts = alert.key.split(separator: ":", maxSplits: 1).map(String.init)
        guard let kind = parts.first else { return (alert.title, alert.text) }
        let subject = parts.count > 1 ? parts[1] : ""
        switch kind {
        case "node":
            guard let state = snapshot.nodes[subject] else { return (alert.title, alert.text) }
            let title = switch state.health {
            case .ready: String(localized: "\(state.hostname) is ready again")
            case .notReady: String(localized: "\(state.hostname) is not ready")
            case .unreachable: String(localized: "\(state.hostname) is unreachable")
            }
            return (title, alert.text)
        case "etcd":
            let alarm = subject.split(separator: ":", maxSplits: 1).map(String.init)
            let text = String(localized: "\(alarm.last ?? subject) on member \(alarm.first ?? "")")
            return (String(localized: "etcd alarm raised"), text)
        case "cert":
            let days = daysUntil(snapshot.certNotAfter, now: now)
            if snapshot.kube {
                let text = days < 0
                    ? String(localized: "The kubeconfig credentials expired \(-days) days ago.")
                    : String(localized: "The kubeconfig credentials expire in \(days) days. Import a new kubeconfig.")
                return (String(localized: "kubeconfig credentials"), text)
            }
            let text = days < 0
                ? String(localized: "The client certificate expired \(-days) days ago.")
                : String(localized: "The client certificate expires in \(days) days. Generate a new talosconfig.")
            return (String(localized: "talosconfig certificate"), text)
        case "data":
            // "system|label": the label names the volume or cluster, the system goes in the text.
            let issue = subject.split(separator: "|", maxSplits: 1).map(String.init)
            let label = issue.last ?? subject
            let system = dataSystemTitle(subject)
            guard alert.problem else { return (String(localized: "\(label) is healthy again"), system) }
            let severity = snapshot.dataIssues[subject] == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
            return (String(localized: "\(label) needs attention"), "\(system) · \(severity)")
        case "gitops":
            return localizedGitOps(alert, subject: subject, value: snapshot.gitopsIssues[subject])
        case "am":
            // "am:fingerprint": named from the issue kept, the current one or, once resolved, the last.
            guard let value = snapshot.amIssues[subject] ?? previous?.amIssues[subject] else { return (alert.title, alert.text) }
            let issue = AMIssue(value: value)
            guard alert.problem else { return (String(localized: "Alertmanager: \(issue.alertname) resolved"), issue.subject) }
            let severity = issue.severity == dataCritical ? AMSeverity.critical.label : AMSeverity.warning.label
            let line = issue.line(severity: severity)
            return (String(localized: "Alertmanager: \(issue.alertname)"), issue.summary.isEmpty ? line : "\(line)\n\(issue.summary)")
        case "checkup":
            // "section|kind|subject": the section names what kind of trouble, the subject what has it.
            let finding = CheckupSubject(key: subject)
            guard alert.problem else { return (CheckupText.monitorCheckupOk(finding.subject), CheckupText.checkupTitle) }
            let section = CheckupSectionID(rawValue: finding.section)?.title ?? finding.section
            let severity = snapshot.checkupIssues[subject] == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
            return ("\(section): \(finding.subject)", "\(CheckupText.checkupTitle) · \(severity)")
        case "storage":
            // "<node>|<volume>:trend": named from the trend kept, the current one or, once resolved, the last.
            if let volume = storageTrendVolumeKey(subject: subject) {
                guard let value = snapshot.storageTrends[volume] ?? previous?.storageTrends[volume] else { return (alert.title, alert.text) }
                return localizedStorageTrend(alert, issue: StorageTrendIssue(value: value))
            }
            // "<node>|…": named from the issue kept, the current one or, once resolved, the last.
            guard let value = snapshot.storageIssues[subject] ?? previous?.storageIssues[subject] else { return (alert.title, alert.text) }
            return localizedStorage(alert, issue: StorageIssue(value: value), warn: snapshot.storageWarn)
        case "unreachable":
            // The cluster is named in the subtitle.
            guard alert.problem else {
                return (String(localized: "Cluster reachable again"), String(localized: "It answers the background checks again."))
            }
            return (String(localized: "Cluster unreachable"), String(localized: "No answer to the last \(unreachableRuns) checks."))
        default:
            return (alert.title, alert.text)
        }
    }

    /// "tool|subject": worded per tool and reason, the namespace/name and severity in the text.
    private static func localizedGitOps(_ alert: Alert, subject key: String, value: String?) -> (title: String, text: String) {
        let subject = GitOpsSubject(key: key)
        let name = subject.title
        guard alert.problem else {
            let title = subject.isFlux
                ? String(localized: "Flux: \(name) is ready again")
                : String(localized: "Argo CD: \(name) is synced and healthy again")
            return (title, subject.label)
        }
        guard let value else { return (alert.title, alert.text) }
        let title = switch gitopsReason(value) {
        case .syncFailed: String(localized: "Argo CD: \(name) sync failed")
        case .degraded: String(localized: "Argo CD: \(name) is degraded")
        case .missing: String(localized: "Argo CD: \(name) has missing resources")
        case .error: String(localized: "Argo CD: \(name) has an error")
        case .outOfSync: String(localized: "Argo CD: \(name) is out of sync")
        case .notReady: String(localized: "Flux: \(name) is not ready")
        case nil: alert.title
        }
        let severity = gitopsSeverity(value) == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
        return (title, "\(subject.label) · \(severity)")
    }

    /// A volume's fill, or a disk failing SMART, on a node named by its hostname.
    private static func localizedStorage(_ alert: Alert, issue: StorageIssue, warn: Int) -> (title: String, text: String) {
        let name = issue.name
        let host = issue.hostname
        if issue.isSmart {
            guard alert.problem else { return (String(localized: "\(name) on \(host) passes SMART again"), "") }
            return (String(localized: "SMART failing on \(name) (\(host))"), issue.detail)
        }
        // The "%" goes with the number, so no key carries a literal one.
        guard alert.problem else {
            let threshold = "\(warn) %"
            return (String(localized: "\(name) on \(host) back under \(threshold)"), "")
        }
        let fill = "\(issue.percent) %"
        let title = issue.severity == dataCritical
            ? String(localized: "\(name) on \(host) at \(fill), almost full")
            : String(localized: "\(name) on \(host) at \(fill)")
        let free = formatBytes(issue.freeBytes)
        let size = formatBytes(issue.sizeBytes)
        return (title, String(localized: "\(free) free of \(size)"))
    }

    /// A volume about to be critical or full, by its growth over the last week.
    private static func localizedStorageTrend(_ alert: Alert, issue: StorageTrendIssue) -> (title: String, text: String) {
        let name = issue.name
        let host = issue.hostname
        guard alert.problem else { return (String(localized: "\(name) on \(host) no longer filling up fast"), "") }
        let when = issue.when.localized
        let title = issue.critical
            ? String(localized: "\(name) on \(host) critical \(when)")
            : String(localized: "\(name) on \(host) full \(when)")
        // The "%" goes with the number, so no key carries a literal one.
        let growth = "\(storagePercentText(issue.slopePerDay)) %"
        let fill = "\(storagePercentText(issue.usedPercent)) %"
        return (title, String(localized: "growing ~\(growth) a day, now at \(fill)"))
    }

    static func requestPermission() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    /// The share link a tapped alert opens, on the cluster checked (its `clusterID`); nil without
    /// one, or for an alert about no screen (the Talos certificate alert opens the renewal).
    private static func alertLink(_ alert: Alert, snapshot: ClusterSnapshot, cluster: String) -> URL? {
        guard !cluster.isEmpty, var target = ShareTarget.forAlert(key: alert.key, snapshot: snapshot) else { return nil }
        target.cluster = cluster
        return TalosClient.shareLink(for: target)
    }

    /// The alert kinds, one notification category each ("alert.node"…), so iOS can group and
    /// summarize them per kind.
    static let alertKinds = ["node", "etcd", "cert", "data", "gitops", "checkup", "alertmanager", "storage", "cluster"]

    /// The category of an alert: its kind's with its `actions` (see alertCategory), "….private"
    /// with details hidden; "private" for a kind this version does not know. Alertmanager alerts
    /// ("am:…") are of the kind "alertmanager", "unreachable" of the kind "cluster" (alertKind).
    static func category(alertKey: String, actions: [AlertAction] = [], hideDetails: Bool) -> String {
        let kind = alertKind(key: alertKey)
        guard alertKinds.contains(kind) else { return hideDetails ? "private" : "" }
        return alertCategory(kind: kind, actions: actions, hideDetails: hideDetails)
    }

    /// With the app lock on, details are hidden on the lock screen (iOS shows "Notification").
    /// `cluster` names the cluster (the subtitle), `id` keeps the same alert of two clusters apart;
    /// `link` (userInfo "link") is the share link the tap opens; `info` what its actions need.
    private static func post(_ alert: Alert, localized: (title: String, text: String), cluster: String, id: String, link: URL?,
                             actions: [AlertAction], info: [String: String], hideDetails: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = localized.title
        content.subtitle = cluster
        content.body = localized.text
        content.sound = alert.problem ? .default : nil
        content.categoryIdentifier = category(alertKey: alert.key, actions: actions, hideDetails: hideDetails)
        var userInfo = info
        if let link { userInfo["link"] = link.absoluteString }
        content.userInfo = userInfo
        let request = UNNotificationRequest(identifier: id, content: content, trigger: nil)
        try? await UNUserNotificationCenter.current().add(request)
    }

    /// Registers a category per alert kind and set of actions (alertActionSets), plain and
    /// ".private": the private ones keep their previews hidden until the device is unlocked. The
    /// title is hidden with the body (no `.hiddenPreviewsShowTitle`): it names the node. The ones
    /// without actions take the resolved alerts and, like "private", the notifications an older
    /// version delivered.
    static func registerCategories() {
        let placeholder = String(localized: "Talos cluster alert")
        func make(_ id: String, actions: [AlertAction], hidden: Bool) -> UNNotificationCategory {
            let buttons = AlertNotificationActions.notificationActions(actions)
            return hidden
                ? UNNotificationCategory(identifier: id, actions: buttons, intentIdentifiers: [],
                                         hiddenPreviewsBodyPlaceholder: placeholder, options: [])
                : UNNotificationCategory(identifier: id, actions: buttons, intentIdentifiers: [], options: [])
        }
        let alerts = alertKinds.flatMap { kind in
            alertActionSets(kind: kind).flatMap { actions in
                [false, true].map { hidden in
                    make(alertCategory(kind: kind, actions: actions, hideDetails: hidden), actions: actions, hidden: hidden)
                }
            }
        }
        let categories = Set(alerts + [make("private", actions: [], hidden: true), FreezeReminders.notificationCategory,
                                       ConfigTryJob.notificationCategory])
        UNUserNotificationCenter.current().setNotificationCategories(categories)
    }
}
