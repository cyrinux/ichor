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

    /// One check: overview + etcd (a cluster added from a kubeconfig: its Kubernetes node list),
    /// diffed with the previous snapshot. Silent if the cluster is unreachable as a whole (e.g. off VPN).
    static func check() async {
        TalosClient.applyStoredPrivacyMask() // also set at launch; keeps this path self-contained
        // Both stores, as the app lists them: the saved position counts kubeconfig clusters too.
        let stored = StoredConfigs.load()
        let parsed = await stored.parsed()
        guard parsed.unreadable.isEmpty, let summary = ConfigSummary.combined(talos: parsed.talos, kube: parsed.kube) else { return }
        let contextName = summary.selectedContext(index: AppModel.savedContextIndex, name: UserDefaults.standard.string(forKey: "activeContext"))
        let context = summary.context(named: contextName)
        // A Talos cluster without an endpoint yet cannot be checked: the widget stops showing the
        // cluster checked before (Android's clearSnapshot).
        if context?.needsEndpoint == true {
            if SharedStore.snapshot() != nil { SharedStore.save(nil) }
            return
        }
        let kube = context?.isKube == true
        guard let yaml = kube ? stored.kube : stored.talos else { return }
        // A VPN-only cluster waits for its VPN: without it the check could only time out.
        let vpnOnly = Set(UserDefaults.standard.stringArray(forKey: "vpnOnlyClusters") ?? [])
        if let fingerprint = context?.fingerprint, vpnOnly.contains(fingerprint) {
            let vpnUp = await VpnMonitor.currentlyUp()
            if heldBackForVpn(vpnOnly: vpnOnly, fingerprint: fingerprint, vpnUp: vpnUp) { return }
        }
        // The Kubernetes API address the user set for this cluster, as the app uses it.
        let servers = UserDefaults.standard.dictionary(forKey: "kubeServers") as? [String: String] ?? [:]
        // And its Kubernetes access: a linked kubeconfig cluster (K5). A sign-in it needs is never
        // started from here: its calls fail and the app asks the user to sign in.
        let links = UserDefaults.standard.dictionary(forKey: "kubeAccess") as? [String: String] ?? [:]
        let link = kube ? nil : stored.kube.flatMap { kube in
            kubeAccessContext(of: context, links: links, kubeContexts: parsed.kube?.contexts ?? []).map { KubeLink(config: kube, context: $0) }
        }
        let client = TalosClient(config: yaml, context: contextName, kubeServer: context.flatMap { servers[$0.fingerprint] } ?? "",
                                 kubeLink: link)
        let kubeAllowed = context?.allows(.workloads, kubeLinked: link != nil) == true
        // The Talos overview, or the Kubernetes node list of a cluster added from a kubeconfig.
        // Unreadable (off VPN, a sign-in needed): nothing to compare, the previous snapshot stays.
        var overview: ClusterOverview?
        var kubeNodes: KubeNodesOverview?
        if kube {
            guard let nodes = try? await client.kubeNodes() else { return }
            kubeNodes = nodes
        } else {
            guard let read = try? await client.overview() else { return }
            overview = read
        }
        var etcd: EtcdOverview?
        if !kube { etcd = try? await client.etcd() }
        // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
        // resources and runs the Garage CLI in a pod.
        let watchData = dataServicesWatched && kubeAllowed
        let dataServices = watchData ? try? await client.dataServices(hints: "") : nil
        // Same gate for Argo CD and Flux apps, read through their custom resources.
        let watchGitOps = gitopsWatched && kubeAllowed
        let previous = SharedStore.snapshot()
        let known = knownGitOpsIssues(previous, context: contextName)
        let gitopsIssues = watchGitOps ? await readGitOps(client, known: known) : nil
        // And for the checkup: it lists the cluster's pods and asks every kubelet.
        let watchCheckup = checkupWatched && kubeAllowed
        let knownCheckup = knownCheckupIssues(previous, context: contextName)
        let checkupIssues = watchCheckup ? await readCheckup(client, known: knownCheckup) : nil
        // And for the Alertmanager, reached like the metrics through the service proxy or a URL.
        let watchAlertmanager = alertmanagerWatched && kubeAllowed
        let knownAlertmanager = knownAlertmanagerIssues(previous, context: contextName)
        let alertmanagerIssues = watchAlertmanager
            ? await readAlertmanager(client, fingerprint: context?.fingerprint ?? "", known: knownAlertmanager) : nil
        let now = Date()
        let current: ClusterSnapshot
        if let kubeNodes {
            current = kubeSnapshotOf(kubeNodes, context: contextName, certNotAfter: context?.certNotAfter ?? 0, takenAt: now,
                                     dataWatched: watchData, dataServices: dataServices,
                                     gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues,
                                     checkupWatched: watchCheckup, checkupIssues: checkupIssues,
                                     alertmanagerWatched: watchAlertmanager, alertmanagerIssues: alertmanagerIssues)
        } else if let overview {
            current = snapshotOf(overview, etcd: etcd, certNotAfter: context?.certNotAfter ?? 0, takenAt: now,
                                 dataWatched: watchData, dataServices: dataServices,
                                 gitopsWatched: watchGitOps, gitopsIssues: gitopsIssues,
                                 checkupWatched: watchCheckup, checkupIssues: checkupIssues,
                                 alertmanagerWatched: watchAlertmanager, alertmanagerIssues: alertmanagerIssues)
        } else {
            return
        }
        let result = evaluate(previous: previous, current: current, now: now)
        SharedStore.save(result.next)
        guard alertsEnabled else { return }
        let hide = UserDefaults.standard.bool(forKey: "appLockEnabled")
        // A snoozed alert posts nothing, neither the problem nor its end (its notification's Snooze).
        let fingerprint = context?.fingerprint ?? ""
        let alerts = AlertSnoozeStore.current(now: now).notSnoozed(result.alerts, cluster: fingerprint, now: now)
        for alert in alerts {
            let link = alertLink(alert, snapshot: result.next, cluster: context?.clusterID ?? "")
            let text = localized(alert, snapshot: result.next, previous: previous, now: now)
            let wake = await canWake(alert, cluster: fingerprint)
            await post(alert, localized: text, link: link, actions: alertActions(key: alert.key, problem: alert.problem, canWake: wake),
                       info: actionInfo(alert, snapshot: result.next, cluster: fingerprint), hideDetails: hide)
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

    /// The share link a tapped alert opens, on the cluster checked (its `clusterID`); nil without
    /// one, or for an alert about no screen (the Talos certificate alert opens the renewal).
    private static func alertLink(_ alert: Alert, snapshot: ClusterSnapshot, cluster: String) -> URL? {
        guard !cluster.isEmpty, var target = ShareTarget.forAlert(key: alert.key, snapshot: snapshot) else { return nil }
        target.cluster = cluster
        return TalosClient.shareLink(for: target)
    }

    /// The alert kinds, one notification category each ("alert.node"…), so iOS can group and
    /// summarize them per kind.
    static let alertKinds = ["node", "etcd", "cert", "data", "gitops", "checkup", "alertmanager"]

    /// The category of an alert: its kind's with its `actions` (see alertCategory), "….private"
    /// with details hidden; "private" for a kind this version does not know. Alertmanager alerts
    /// ("am:…") are of the kind "alertmanager".
    static func category(alertKey: String, actions: [AlertAction] = [], hideDetails: Bool) -> String {
        let prefix = String(alertKey.split(separator: ":", maxSplits: 1).first ?? "")
        let kind = prefix == "am" ? "alertmanager" : prefix
        guard alertKinds.contains(kind) else { return hideDetails ? "private" : "" }
        return alertCategory(kind: kind, actions: actions, hideDetails: hideDetails)
    }

    /// With the app lock on, details are hidden on the lock screen (iOS shows "Notification").
    /// `link` (userInfo "link") is the share link the tap opens; `info` what its actions need.
    private static func post(_ alert: Alert, localized: (title: String, text: String), link: URL?, actions: [AlertAction],
                             info: [String: String], hideDetails: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = localized.title
        content.body = localized.text
        content.sound = alert.problem ? .default : nil
        content.categoryIdentifier = category(alertKey: alert.key, actions: actions, hideDetails: hideDetails)
        var userInfo = info
        if let link { userInfo["link"] = link.absoluteString }
        content.userInfo = userInfo
        let request = UNNotificationRequest(identifier: alert.key, content: content, trigger: nil)
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
        let categories = Set(alerts + [make("private", actions: [], hidden: true), FreezeReminders.notificationCategory])
        UNUserNotificationCenter.current().setNotificationCategories(categories)
    }
}
