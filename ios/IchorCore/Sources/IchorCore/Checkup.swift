import Foundation

// Mirrors go/ichorgo/kube_checkup*.go and kube_events.go: the cluster checkup (what no other
// screen shows, section by section), an object's events, and the pure logic of both.

/// A section's or the whole checkup's state, worst first.
public enum CheckupStatus: String, Sendable, WireEnum {
    case critical, warning, ok, unknown, absent

    public static let wireFallback: Self = .unknown
}

public enum CheckupSeverity: String, Sendable, WireEnum {
    case critical, warning, info

    public static let wireFallback: Self = .info
}

/// The sections of a checkup, as Go names them.
public enum CheckupSectionID: String, Sendable {
    case workloads, events, storage, upgrade, webhooks, policies, capacity, nodes, loadbalancers, terminating, certificates, secrets, helm, monitoring, crossplane, tailscale, clusterapi
    /// The findings the plugins of the apps the cluster runs declare (docs/plugins.md "Findings").
    case plugins
}

/// What a finding is about, as Go names it.
public enum CheckupKind: String, Sendable {
    case podCrashLoop, podImagePull, podUnschedulable, podStuckStarting, podNotReady, podFailed, podOOMKilled, jobFailed
    case event
    case volumeFull, volumeInodes, pvcPending, pvcLost, pvFailed, pvReleased
    case deprecatedAPI, webhookDown, webhookSkipped
    /// The admission-policy engine cannot admit, a policy not ready, a workload a policy
    /// denies, an Audit policy whose background scans keep failing.
    case policyEngineDown, policyNotReady, policyBlocks, policyFailures
    case nodeRequestsHigh, nodePodsFull, noRoomToDrain, quotaNearLimit
    case nodeNotReady, nodePressure, nodeCordoned, nodeVersionSkew
    case lbPending, lbPoolExhausted, lbPoolConflict, lbPoolUnadvertised, lbConfigError, lbBGPPeerDown
    case namespaceTerminating, podTerminating, pvcTerminating
    /// A network test namespace the app could not delete (it was closed mid-run).
    case netperfLeftover
    case csrPending, csrDenied
    case externalSecretFailed, secretStoreNotReady
    case helmFailed, helmPending
    case scrapeTargetsDown, prometheusRuleErrors, ruleGroupErrors
    /// A Crossplane resource that cannot sync, a package not healthy, a resource not ready past
    /// the grace, a definition not established. Name: "Kind/name".
    case crossplaneSyncFailed, crossplaneProviderUnhealthy, crossplaneNotReady, crossplaneDefinitionNotEstablished
    /// The Tailscale operator down (extra: "ready/desired"), a Connector not ready (extra: its
    /// roles), a ProxyGroup with proxies down (extra: its type), a ProxyClass in use not ready
    /// (count: its users).
    case tailscaleOperatorDown, tailscaleConnectorNotReady, tailscaleProxyGroupDown, tailscaleProxyClassNotReady
    /// On a management cluster: a workload cluster failed or still provisioning past an hour, a
    /// Machine failed or stuck (extra: "cluster/phase"), a MachineDeployment short of available
    /// replicas (value of limit), a Sidero server in use but off, one not accepted (name "" and
    /// count: that many more).
    case capiClusterFailed, capiClusterProvisioning, capiMachineFailed, capiMachineStuck, capiWorkersShort
    case sideroServerOff, sideroServerUnaccepted
    /// A SealedSecret the controller could not decrypt, the controller without a key (extra: its
    /// namespace), a sealing key kept out of use (namespace, name: the key's Secret).
    case sealedSecretNotSynced, sealingKeyMissing, sealingKeyCompromised
    /// A Flux Kustomization that cannot decrypt its SOPS files (extra: its decryption secretRef,
    /// "" when not configured; reason, message: its Ready condition's).
    case sopsDecryptionFailed
    /// A certificate a route's host serves (read by TLS handshake) that expired, expires within
    /// 14 days (count: days left) or does not name the host (message: the names it covers).
    /// Namespace, name: its Secret; extra: the route; reason: the host.
    case routeCertExpired, routeCertExpiring, routeCertHostMismatch
    /// A finding a plugin declares, in the plugin's words (`plugin`, already translated by Go);
    /// namespace and name are its object's.
    case plugin
}

/// What the cluster hides behind green nodes: one section per kind of trouble.
public struct CheckupReport: Decodable, Equatable, Sendable {
    public let status: CheckupStatus
    public let kubeVersion: String
    public let sections: [CheckupSection]
    public let nodes: [CheckupNode]
    public let volumes: [CheckupVolume]
    public let releases: [CheckupRelease]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = try c.wire(.status)
        kubeVersion = try c.field(.kubeVersion, "")
        sections = try c.field(.sections, [])
        nodes = try c.field(.nodes, [])
        volumes = try c.field(.volumes, [])
        releases = try c.field(.releases, [])
    }

    private enum CodingKeys: String, CodingKey { case status, kubeVersion, sections, nodes, volumes, releases }

    /// The sections worth showing: what the cluster does not run is left out.
    public var shownSections: [CheckupSection] { sections.filter { $0.status != .absent } }

    /// How many findings of `severity` the whole report holds.
    public func count(_ severity: CheckupSeverity) -> Int {
        sections.reduce(0) { $0 + $1.count(severity) }
    }

    /// The findings worth a background alert, keyed "section|kind|subject" and valued by
    /// severity: everything but the notes and the events, which only describe, and the kinds the
    /// data services already alert on (`dataServiceAlertedKinds`).
    public var alertIssues: [String: String] { alertIssues(gitopsWatched: false) }

    /// `alertIssues`, leaving out as well the kinds the GitOps alert already raises
    /// (`gitopsAlertedKinds`) when that track is watched, and the plugin findings when the
    /// plugins' own track (pluginIssuesOf) is watched.
    /// An imported plugin's finding alerts only with `importedPluginsWatched`.
    public func alertIssues(gitopsWatched: Bool, pluginsWatched: Bool = false, importedPluginsWatched: Bool = false) -> [String: String] {
        var skipped = gitopsWatched ? dataServiceAlertedKinds.union(gitopsAlertedKinds) : dataServiceAlertedKinds
        if pluginsWatched { skipped.insert(CheckupKind.plugin.rawValue) }
        var issues: [String: String] = [:]
        for section in sections where section.id != CheckupSectionID.events.rawValue {
            for finding in section.findings where finding.severity != .info && !skipped.contains(finding.kindName)
                && !(finding.plugin?.user == true && !importedPluginsWatched) {
                let key = "\(section.id)|\(finding.kindName)|\(finding.alertSubject)"
                // Two plugin findings on one object: the worst one.
                if issues[key] == CheckupSeverity.critical.rawValue { continue }
                issues[key] = finding.severity.rawValue
            }
        }
        return issues
    }
}

/// One check: `checked` objects looked at, `truncated` findings left out, `error` when unreadable.
public struct CheckupSection: Decodable, Equatable, Sendable, Identifiable {
    public let id: String
    public let status: CheckupStatus
    public let error: String
    public let checked: Int
    public let findings: [CheckupFinding]
    public let truncated: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        status = try c.wire(.status)
        error = try c.field(.error, "")
        checked = try c.field(.checked, 0)
        findings = try c.field(.findings, [])
        truncated = try c.field(.truncated, 0)
    }

    private enum CodingKeys: String, CodingKey { case id, status, error, checked, findings, truncated }

    /// Nil for a section this version does not know.
    public var section: CheckupSectionID? { CheckupSectionID(rawValue: id) }

    public func count(_ severity: CheckupSeverity) -> Int { findings.filter { $0.severity == severity }.count }
}

/// One problem. `reason` and `message` are Kubernetes' own words; the rest depends on the kind.
public struct CheckupFinding: Decodable, Equatable, Sendable {
    /// The kind as Go names it; `kind` is nil for one this version does not know.
    public let kindName: String
    public let severity: CheckupSeverity
    public let namespace: String
    public let name: String
    public let node: String
    public let reason: String
    public let message: String
    public let extra: String
    public let value: Double
    public let limit: Double
    public let count: Int
    /// Unix ms, 0 when unknown.
    public let since: Int64
    /// Kind `plugin`: the plugin, its finding's words (already translated) and where it leads;
    /// nil for the other kinds.
    public let plugin: PluginFindingInfo?

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kindName = try c.field(.kind, "")
        severity = try c.wire(.severity)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        node = try c.field(.node, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        extra = try c.field(.extra, "")
        value = try c.field(.value, 0)
        limit = try c.field(.limit, 0)
        count = try c.field(.count, 0)
        since = try c.field(.since, 0)
        plugin = if kindName == CheckupKind.plugin.rawValue { try? PluginFindingInfo(from: decoder) } else { nil }
    }

    private enum CodingKeys: String, CodingKey { case kind, severity, namespace, name, node, reason, message, extra, value, limit, count, since }

    public var kind: CheckupKind? { CheckupKind(rawValue: kindName) }

    /// "namespace/name", or the name alone for what has no namespace.
    public var subject: String { namespace.isEmpty ? name : "\(namespace)/\(name)" }

    /// What a background alert names: the subject, else (a plugin finding about no object) the
    /// finding's title.
    var alertSubject: String { subject.isEmpty ? plugin?.headline ?? "" : subject }

    /// Kubernetes' own words about the finding, "" when it has none: the reason where it says
    /// more than the title does, then the message.
    public var detail: String {
        if let plugin { return plugin.detail }
        let reasonKinds: Set<CheckupKind> = [.podCrashLoop, .podOOMKilled, .podStuckStarting, .jobFailed, .nodeNotReady, .nodePressure,
                                             .externalSecretFailed, .secretStoreNotReady, .prometheusRuleErrors, .ruleGroupErrors,
                                             .crossplaneSyncFailed, .crossplaneProviderUnhealthy, .crossplaneNotReady,
                                             .crossplaneDefinitionNotEstablished, .tailscaleConnectorNotReady,
                                             .tailscaleProxyGroupDown, .tailscaleProxyClassNotReady, .capiClusterFailed,
                                             .capiClusterProvisioning, .capiMachineFailed, .capiMachineStuck, .sealedSecretNotSynced,
                                             .sopsDecryptionFailed]
        let shownReason = kind.map(reasonKinds.contains) == true ? reason : ""
        return [shownReason, message].filter { !$0.isEmpty }.joined(separator: ": ")
    }
}

/// A node as Kubernetes sees it: requests against allocatable (cores, bytes), taints and labels.
public struct CheckupNode: Decodable, Equatable, Sendable, Identifiable {
    public let name: String
    public let roles: [String]
    public let ready: Bool
    public let cordoned: Bool
    public let kubelet: String
    public let taints: [String]
    public let labels: [String]
    public let cpuRequests: Double
    public let cpuAllocatable: Double
    public let cpuPercent: Double
    public let memoryRequests: Double
    public let memoryAllocatable: Double
    public let memoryPercent: Double
    public let pods: Int
    public let podCapacity: Int

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        roles = try c.field(.roles, [])
        ready = try c.field(.ready, false)
        cordoned = try c.field(.cordoned, false)
        kubelet = try c.field(.kubelet, "")
        taints = try c.field(.taints, [])
        labels = try c.field(.labels, [])
        cpuRequests = try c.field(.cpuRequests, 0)
        cpuAllocatable = try c.field(.cpuAllocatable, 0)
        cpuPercent = try c.field(.cpuPercent, 0)
        memoryRequests = try c.field(.memoryRequests, 0)
        memoryAllocatable = try c.field(.memoryAllocatable, 0)
        memoryPercent = try c.field(.memoryPercent, 0)
        pods = try c.field(.pods, 0)
        podCapacity = try c.field(.podCapacity, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case name, roles, ready, cordoned, kubelet, taints, labels, cpuRequests, cpuAllocatable, cpuPercent
        case memoryRequests, memoryAllocatable, memoryPercent, pods, podCapacity
    }
}

/// A PersistentVolumeClaim and how full it is; not `measured` when no kubelet reported it.
public struct CheckupVolume: Decodable, Equatable, Sendable, Identifiable {
    public let namespace: String
    public let name: String
    public let phase: String
    public let storageClass: String
    public let capacity: Double
    public let used: Double
    public let usedPercent: Double
    public let inodesPercent: Double
    public let measured: Bool
    public let pod: String
    public let node: String

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        storageClass = try c.field(.storageClass, "")
        capacity = try c.field(.capacity, 0)
        used = try c.field(.used, 0)
        usedPercent = try c.field(.usedPercent, 0)
        inodesPercent = try c.field(.inodesPercent, 0)
        measured = try c.field(.measured, false)
        pod = try c.field(.pod, "")
        node = try c.field(.node, "")
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, phase, storageClass, capacity, used, usedPercent, inodesPercent, measured, pod, node
    }
}

/// A Helm release at its latest revision.
public struct CheckupRelease: Decodable, Equatable, Sendable, Identifiable {
    public let namespace: String
    public let name: String
    public let status: String
    public let revision: Int
    public let updated: Int64

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        status = try c.field(.status, "")
        revision = try c.field(.revision, 0)
        updated = try c.field(.updated, 0)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, status, revision, updated }

    public var inTrouble: Bool { status == "failed" || status.hasPrefix("pending-") }
}

/// A Kubernetes event of an object, as `kubectl describe` ends with.
public struct KubeEvent: Decodable, Equatable, Sendable {
    public let type: String
    public let reason: String
    public let message: String
    public let kind: String
    public let namespace: String
    public let name: String
    public let count: Int
    public let first: Int64
    public let last: Int64
    public let source: String
    /// The admission policy that caused a FailedCreate denial, when one did.
    public let policy: EventPolicy?

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        count = try c.field(.count, 1)
        first = try c.field(.first, 0)
        last = try c.field(.last, 0)
        source = try c.field(.source, "")
        policy = try c.decodeIfPresent(EventPolicy.self, forKey: .policy)
    }

    private enum CodingKeys: String, CodingKey { case type, reason, message, kind, namespace, name, count, first, last, source, policy }

    public var isWarning: Bool { type != "Normal" }
}

/// The admission policy one denial event names (Go policy_denial.go).
public struct EventPolicy: Decodable, Equatable, Sendable {
    public let source: String
    public let policy: String
    public let rule: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        source = try c.field(.source, "")
        policy = try c.field(.policy, "")
        rule = try c.field(.rule, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case source, policy, rule, message }
}

public struct KubeEventList: Decodable, Equatable, Sendable {
    public let events: [KubeEvent]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        events = try c.field(.events, [])
    }

    private enum CodingKeys: String, CodingKey { case events }
}

// MARK: - Background alerts

/// What a checkup alert key ("section|kind|subject") is about.
public struct CheckupSubject: Equatable, Sendable {
    public let section: String
    public let kind: String
    public let subject: String

    public init(key: String) {
        let parts = key.split(separator: "|", maxSplits: 2, omittingEmptySubsequences: false).map(String.init)
        section = parts.first ?? ""
        kind = parts.count > 1 ? parts[1] : ""
        subject = parts.count > 2 ? parts[2] : ""
    }
}

/// Checkup finding kinds whose problem the data services alert already raises (`dataIssuesOf`):
/// shown on the checkup screen, never notified a second time. A SealedSecret that failed to unseal
/// and no active sealing key alert from the Sealed Secrets section.
public let dataServiceAlertedKinds: Set<String> = [CheckupKind.sealedSecretNotSynced.rawValue, CheckupKind.sealingKeyMissing.rawValue]

/// Checkup finding kinds whose problem the GitOps alert already raises when it is on
/// (`gitopsIssuesWithGaps`): a Kustomization that cannot decrypt its SOPS files is Ready=False, a
/// Flux app the GitOps track notifies about.
public let gitopsAlertedKinds: Set<String> = [CheckupKind.sopsDecryptionFailed.rawValue]

/// The checkup findings worth a notification, for a report where a section could not be read:
/// that section keeps the issues `known` had for it, so they neither clear falsely nor come back
/// as new once it reads again. With `gitopsWatched`, the kinds the GitOps alert already raises
/// are left to it; with `pluginsWatched`, the plugin findings to the plugins' track.
public func checkupIssuesWithGaps(_ report: CheckupReport, known: [String: String], gitopsWatched: Bool = false,
                                  pluginsWatched: Bool = false, importedPluginsWatched: Bool = false) -> [String: String] {
    let unread = report.sections.filter { !$0.error.isEmpty }.map { "\($0.id)|" }
    var issues = report.alertIssues(gitopsWatched: gitopsWatched, pluginsWatched: pluginsWatched,
                                    importedPluginsWatched: importedPluginsWatched)
    let pluginKind = "|\(CheckupKind.plugin.rawValue)|"
    for (key, value) in known where issues[key] == nil && unread.contains(where: key.hasPrefix) && !(pluginsWatched && key.contains(pluginKind)) {
        issues[key] = value
    }
    return issues
}

/// The checkup issues a partial read may carry over: the previous snapshot's, only when it is the
/// same cluster and that check watched and read them. Another cluster's issues must never leak in.
public func knownCheckupIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.checkupWatched, previous.checkupChecked else { return [:] }
    return previous.checkupIssues
}

/// Checkup findings (see evaluateTrack), keyed "checkup:section|kind|subject".
func evaluateCheckup(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]) -> IssueTrack {
    evaluateTrack(
        previous: previous?.checkupTrack, current: current.checkupTrack, comparable: comparable, severity: { $0 },
        problem: { key, severity in
            let subject = CheckupSubject(key: key)
            return Alert(key: "checkup:\(key)", title: "\(subject.section): \(subject.subject)", text: "Cluster checkup · \(severity)", problem: true)
        },
        cleared: { key in
            Alert(key: "checkup:\(key)", title: "Resolved: \(CheckupSubject(key: key).subject)", text: "Cluster checkup", problem: false)
        },
        alerts: &alerts)
}
