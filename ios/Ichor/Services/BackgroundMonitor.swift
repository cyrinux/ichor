import BackgroundTasks
import Foundation
import IchorCore
import UserNotifications
import WidgetKit

/// Shared with the widget extension (App Group).
enum SharedStore {
    private static let snapshotKey = SharedSnapshot.key

    static var defaults: UserDefaults { UserDefaults(suiteName: SharedSnapshot.suite) ?? .standard }

    static func snapshot() -> ClusterSnapshot? {
        SharedSnapshot.load(from: defaults)
    }

    static func save(_ snapshot: ClusterSnapshot?) {
        if let snapshot, let data = try? JSONEncoder().encode(snapshot) {
            defaults.set(data, forKey: snapshotKey)
        } else {
            defaults.removeObject(forKey: snapshotKey)
        }
        WidgetCenter.shared.reloadAllTimelines()
    }
}

/// Best-effort background checks: iOS decides when refresh tasks run (often every 15-60
/// minutes). The config can only be decrypted while the device is unlocked, so checks made
/// while it is locked are skipped. Same alert rules as Android (IchorCore.evaluate).
enum BackgroundMonitor {
    static let taskID = "name.levis.ichor.refresh"
    static let alertsKey = "monitor.alerts"

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

    /// One check: overview + etcd, diffed with the previous snapshot. Silent if the cluster
    /// is unreachable as a whole (e.g. off VPN).
    static func check() async {
        TalosClient.applyStoredPrivacyMask() // also set at launch; keeps this path self-contained
        guard let data = SecureConfigStore.load(), let yaml = String(data: data, encoding: .utf8),
              let summary = try? await TalosClient.parse(yaml) else { return }
        let contextName = summary.selectedContext(index: AppModel.savedContextIndex, name: UserDefaults.standard.string(forKey: "activeContext"))
        let context = summary.context(named: contextName)
        // A VPN-only cluster waits for its VPN: without it the check could only time out.
        let vpnOnly = Set(UserDefaults.standard.stringArray(forKey: "vpnOnlyClusters") ?? [])
        if let fingerprint = context?.fingerprint, vpnOnly.contains(fingerprint) {
            let vpnUp = await VpnMonitor.currentlyUp()
            if heldBackForVpn(vpnOnly: vpnOnly, fingerprint: fingerprint, vpnUp: vpnUp) { return }
        }
        // The Kubernetes API address the user set for this cluster, as the app uses it.
        let servers = UserDefaults.standard.dictionary(forKey: "kubeServers") as? [String: String] ?? [:]
        let client = TalosClient(config: yaml, context: contextName, kubeServer: context.flatMap { servers[$0.fingerprint] } ?? "")
        guard let overview = try? await client.overview() else { return }
        let etcd = try? await client.etcd()
        // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
        // resources and runs the Garage CLI in a pod.
        let watchData = dataServicesWatched && context?.allows(.workloads) == true
        let dataServices = watchData ? try? await client.dataServices(hints: "") : nil
        // Same gate for Argo CD and Flux apps, read through their custom resources.
        let watchGitOps = gitopsWatched && context?.allows(.workloads) == true
        let previous = SharedStore.snapshot()
        let known = knownGitOpsIssues(previous, context: overview.context)
        let gitopsIssues = watchGitOps ? await readGitOps(client, known: known) : nil
        let now = Date()
        let current = snapshotOf(overview, etcd: etcd, certNotAfter: context?.certNotAfter ?? 0, takenAt: now,
                                 dataWatched: watchData, dataServices: dataServices,
                                 gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues)
        let result = evaluate(previous: previous, current: current, now: now)
        SharedStore.save(result.next)
        guard alertsEnabled else { return }
        let hide = UserDefaults.standard.bool(forKey: "appLockEnabled")
        for alert in result.alerts {
            await post(alert, localized: localized(alert, snapshot: result.next, now: now), hideDetails: hide)
        }
    }

    /// Argo CD and Flux issues (a tool not installed reads fine and adds nothing); nil when neither
    /// could be read. A part that could not be read keeps what the last snapshot knew of it.
    private static func readGitOps(_ client: TalosClient, known: [String: String]) async -> [String: String]? {
        async let argo = try? client.argoCD()
        async let flux = try? client.flux()
        return gitopsIssuesWithGaps(argo: await argo, flux: await flux, known: known)
    }

    /// IchorCore builds English alerts; this rebuilds their text in the user's
    /// language from the alert key and the snapshot (same wording, keys in Localizable.xcstrings).
    static func localized(_ alert: Alert, snapshot: ClusterSnapshot, now: Date) -> (title: String, text: String) {
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

    static func requestPermission() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    /// With the app lock on, details are hidden on the lock screen (iOS shows "Notification").
    private static func post(_ alert: Alert, localized: (title: String, text: String), hideDetails: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = localized.title
        content.body = localized.text
        content.sound = alert.problem ? .default : nil
        if hideDetails { content.categoryIdentifier = "private" }
        let request = UNNotificationRequest(identifier: alert.key, content: content, trigger: nil)
        try? await UNUserNotificationCenter.current().add(request)
    }

    /// Registers the "private" category: its previews stay hidden until the device is unlocked.
    /// The title is hidden with the body (no `.hiddenPreviewsShowTitle`): it names the node.
    static func registerCategories() {
        let category = UNNotificationCategory(identifier: "private", actions: [], intentIdentifiers: [],
                                              hiddenPreviewsBodyPlaceholder: String(localized: "Talos cluster alert"),
                                              options: [])
        UNUserNotificationCenter.current().setNotificationCategories([category, FreezeReminders.notificationCategory])
    }
}
