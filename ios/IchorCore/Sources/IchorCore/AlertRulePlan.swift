import Foundation

// What the background check does with its alerts under the alert rules: what Go is asked (the
// kind and severity of each alert, same table as Android's monitor/AlertRulePlan.kt) and how
// its answer splits the alerts into posted, muted and digested ones.

/// The kind Go reads for an alert's rule, from its key: "node", "etcd" and "unreachable" are
/// critical when the severity is "" (Go's default by kind); `kube`: the cluster was added from
/// a kubeconfig (its "cert" alert is about the kubeconfig's credentials).
public func alertRuleKind(key: String, kube: Bool = false) -> String {
    switch key {
    case "cert": return kube ? "kubeconfig" : "cert"
    case "unreachable": return "unreachable"
    case signInAlertKey: return "signin"
    default: break
    }
    let prefix = String(key.split(separator: ":", maxSplits: 1).first ?? Substring(key))
    if prefix == "storage", key.hasSuffix(":trend") { return "trend" }
    return prefix
}

/// How bad an alert is for the minimum severity and "except critical", from what the snapshot
/// keeps of it (`snapshot`, or `previous` for one that resolved): "" for nodes, etcd and an
/// unreachable cluster (Go makes them critical), an Alertmanager alert's own severity, a pull
/// request info, a "sign in again" a warning, an expired certificate or kubeconfig critical.
public func alertRuleSeverity(_ alert: Alert, snapshot: ClusterSnapshot?, previous: ClusterSnapshot? = nil, now: Date)
    -> String
{
    let parts = alert.key.split(separator: ":", maxSplits: 1).map(String.init)
    let subject = parts.count > 1 ? parts[1] : ""
    func kept(_ issues: (ClusterSnapshot) -> [String: String], _ key: String) -> String? {
        snapshot.flatMap { issues($0)[key] } ?? previous.flatMap { issues($0)[key] }
    }
    func level(_ severity: String) -> String { severity == dataCritical ? alertSeverityCritical : alertSeverityWarning }
    switch alertRuleKind(key: alert.key) {
    case "node", "etcd", "unreachable": return ""
    case "cert":
        guard let notAfter = snapshot?.certNotAfter, notAfter > 0 else { return alertSeverityWarning }
        return daysUntil(notAfter, now: now) < 0 ? alertSeverityCritical : alertSeverityWarning
    case "signin": return alertSeverityWarning
    case "proposal": return alertSeverityInfo
    case "data": return kept({ $0.dataIssues }, subject) ?? ""
    case "gitops": return kept({ $0.gitopsIssues }, subject).map(gitopsSeverity) ?? ""
    case "checkup": return kept({ $0.checkupIssues }, subject) ?? ""
    case "am": return kept({ $0.amIssues }, subject).map { AMIssue(value: $0).severity.lowercased() } ?? ""
    case "storage": return kept({ $0.storageIssues }, subject).map { level(StorageIssue(value: $0).severity) } ?? ""
    case "trend":
        let volume = storageTrendVolumeKey(subject: subject) ?? subject
        guard let value = kept({ $0.storageTrends }, volume) else { return alertSeverityWarning }
        return StorageTrendIssue(value: value).critical ? alertSeverityCritical : alertSeverityWarning
    case "sensor": return kept({ $0.sensorIssues }, subject).map { level(SensorIssue(value: $0).severity) } ?? ""
    case "plugin": return kept({ $0.pluginIssues }, alert.key).map { PluginIssue(value: $0).severity } ?? ""
    default: return ""
    }
}

/// What a run does with one alert under the rules.
public enum AlertRuleOutcome: Equatable, Sendable {
    /// Posted on its own, as before the rules.
    case notify
    /// Nothing posted, nothing recorded: snoozed from its notification.
    case drop
    /// Kept quiet by the quiet hours or the minimum severity (`reason`), recorded for the
    /// history; `until` the quiet window's end (unix ms), else 0.
    case mute(reason: String, until: Int64)
    /// Counted in its cluster's digest notification.
    case digest
    /// An ongoing alert posted again: its repeat is due.
    case `repeat`
}

/// One outcome per input in order, and the digests to post (a cluster with digested alerts).
/// `repeats`: per input, whether a posted one will repeat (it gets Acknowledge).
public struct AlertRulePlan: Equatable, Sendable {
    public let outcomes: [AlertRuleOutcome]
    public let digests: [AlertDigest]
    public let repeats: [Bool]

    public init(outcomes: [AlertRuleOutcome], digests: [AlertDigest] = [], repeats: [Bool]? = nil) {
        self.outcomes = outcomes
        self.digests = digests
        self.repeats = repeats ?? outcomes.map { _ in false }
    }
}

/// What to do with each of `inputs` given Go's `decisions` (nil: the rules could not be
/// applied, or there are none), as Android's alertPlan: without decisions every alert not
/// snoozed is posted, so a rules error never drops an alert; so is one Go gave no matching
/// decision for, and one sent to a digest Go did not count. A snoozed one is dropped either
/// way; one muted by the quiet hours or the minimum severity is kept for the history. An ongoing
/// one (see AlertInput) is posted again only on Go's "repeat": never as a fallback.
public func alertRulePlan(inputs: [AlertInput], decisions: AlertDecisions?) -> AlertRulePlan {
    let digests = decisions?.digests ?? []
    let counted = Dictionary(digests.map { ($0.cluster, $0) }, uniquingKeysWith: { first, _ in first })
    var digestOrder: [String] = []
    let outcomes = inputs.enumerated().map { index, input -> AlertRuleOutcome in
        let all = decisions?.decisions ?? []
        guard index < all.count, all[index].cluster == input.cluster, all[index].key == input.key else {
            return input.snoozed || input.ongoing ? .drop : .notify
        }
        let decision = all[index]
        if input.ongoing { return decision.action == alertActionRepeat ? .repeat : .drop }
        switch decision.action {
        case alertActionMute:
            let recorded = decision.reason == alertReasonQuiet || decision.reason == alertReasonSeverity
            return recorded ? .mute(reason: decision.reason, until: decision.until) : .drop
        case alertActionDigest:
            // Sent to a digest Go did not count: posted on its own rather than lost.
            guard counted[input.cluster] != nil else { return .notify }
            if !digestOrder.contains(input.cluster) { digestOrder.append(input.cluster) }
            return .digest
        default:
            return .notify
        }
    }
    let all = decisions?.decisions ?? []
    let repeats = inputs.indices.map { index in
        index < all.count && all[index].cluster == inputs[index].cluster && all[index].key == inputs[index].key
            && all[index].repeats
    }
    return AlertRulePlan(outcomes: outcomes, digests: digestOrder.compactMap { counted[$0] }, repeats: repeats)
}

/// The notification key of a history alert span (the ring keeps the snapshot's key and its
/// track apart): "data:<key>", "am:<key>"…; "cert" for the certificate.
public func historyAlertKey(track: String, key: String) -> String {
    track == HistoryTrack.cert ? "cert" : "\(track):\(key)"
}
