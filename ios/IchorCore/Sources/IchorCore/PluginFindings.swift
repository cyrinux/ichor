import Foundation

// Mirrors go/ichorgo/plugin_findings.go and kube_checkup_plugins.go (docs/plugins.md
// "Findings"): a plugin's findings as the cluster checkup's plugins section and the background
// monitor (KubePluginFindings) carry them, and the monitor's issues built from them. Titles and
// details come already translated from Go, notifications included.

/// Who raised a plugin finding, its own words and where it leads; every field may be absent.
public struct PluginFindingInfo: Decodable, Equatable, Sendable {
    /// "plugin:<plugin id>:<finding id>:<namespace>/<name>": stable from one read to the next.
    public let key: String
    /// The plugin's id, name and icon.
    public let plugin: String
    public let pluginName: String
    public let pluginIcon: String
    /// The finding's id.
    public let id: String
    public let title: String
    /// "" when the finding has none.
    public let detail: String
    public let ref: PluginRef?
    /// The card the finding links to.
    public let opens: String
    /// The plugin was imported, not built in: notified only when the user said so.
    public let user: Bool

    public init(key: String = "", plugin: String = "", pluginName: String = "", pluginIcon: String = "", id: String = "",
                title: String = "", detail: String = "", ref: PluginRef? = nil, opens: String = "", user: Bool = false) {
        self.key = key
        self.plugin = plugin
        self.pluginName = pluginName
        self.pluginIcon = pluginIcon
        self.id = id
        self.title = title
        self.detail = detail
        self.ref = ref
        self.opens = opens
        self.user = user
    }

    private enum CodingKeys: String, CodingKey { case key, plugin, pluginName, pluginIcon, id, title, detail, ref, opens, user }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plugin = try c.field(.plugin, "")
        pluginName = try c.field(.pluginName, "")
        pluginIcon = try c.field(.pluginIcon, "")
        id = try c.field(.id, "")
        title = try c.field(.title, "")
        detail = try c.field(.detail, "")
        ref = try? c.decodeIfPresent(PluginRef.self, forKey: .ref)
        opens = try c.field(.opens, "")
        user = try c.field(.user, false)
        let sent: String = try c.field(.key, "")
        key = sent.isEmpty ? pluginFindingKey(plugin: plugin, finding: id, ref: ref) : sent
    }

    /// The plugin's name, its id when Go sent none.
    public var pluginLabel: String { pluginName.isEmpty ? plugin : pluginName }

    /// What the finding names: its title, else the object.
    public var headline: String { title.isEmpty ? ref?.label ?? id : title }
}

/// Go's pluginFindingKey: "plugin:<plugin>:<finding>:<namespace>/<name>", "…:<name>" for a
/// cluster-scoped object, "plugin:<plugin>:<finding>" without one.
public func pluginFindingKey(plugin: String, finding: String, ref: PluginRef?) -> String {
    let key = "plugin:\(plugin):\(finding)"
    guard let ref else { return key }
    return ref.namespace.isEmpty ? "\(key):\(ref.name)" : "\(key):\(ref.namespace)/\(ref.name)"
}

/// The plugin id of a finding key ("plugin:<id>:…"), "" for another key.
public func pluginIDOfFindingKey(_ key: String) -> String {
    let parts = key.split(separator: ":", maxSplits: 2, omittingEmptySubsequences: false)
    guard parts.count >= 2, parts[0] == "plugin" else { return "" }
    return String(parts[1])
}

/// One finding of KubePluginFindings.
public struct PluginFindingItem: Decodable, Equatable, Sendable {
    public let severity: CheckupSeverity
    public let info: PluginFindingInfo
    /// A section of the checkup reports it from its own reader (External Secrets): an app that
    /// also watches the checkup skips it, or the same object alerts twice.
    public let checkupNative: Bool

    private enum CodingKeys: String, CodingKey { case severity, checkupNative }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        severity = try c.wire(.severity)
        checkupNative = try c.field(.checkupNative, false)
        info = try PluginFindingInfo(from: decoder)
    }
}

/// A plugin that could not be read in full (`resource` set) or at all.
public struct PluginFindingsError: Decodable, Equatable, Sendable {
    public let plugin: String
    public let pluginName: String
    public let resource: String
    public let message: String

    private enum CodingKeys: String, CodingKey { case plugin, pluginName, resource, message }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plugin = try c.field(.plugin, "")
        pluginName = try c.field(.pluginName, "")
        resource = try c.field(.resource, "")
        message = try c.field(.message, "")
    }
}

/// KubePluginFindings: every active plugin's findings, worst first, at most 200 (`truncated`
/// counts the rest), and the plugins that could not be read.
public struct PluginFindingsReport: Decodable, Equatable, Sendable {
    public let plugins: Int
    public let findings: [PluginFindingItem]
    public let truncated: Int
    public let errors: [PluginFindingsError]

    private enum CodingKeys: String, CodingKey { case plugins, findings, truncated, errors }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plugins = try c.field(.plugins, 0)
        // One finding a newer core words differently must not hide the others.
        findings = (try c.field(.findings, [Lenient<PluginFindingItem>]())).compactMap(\.value)
        truncated = try c.field(.truncated, 0)
        errors = (try c.field(.errors, [Lenient<PluginFindingsError>]())).compactMap(\.value)
    }
}

/// An element decoded on its own: nil when it cannot be.
private struct Lenient<T: Decodable>: Decodable {
    let value: T?

    init(from decoder: Decoder) throws { value = try? T(from: decoder) }
}

// MARK: - Background alerts

/// A plugin issue as a snapshot keeps it (keyed by the finding's `key`): its severity and the
/// words Go gave it, so the notification (and the one saying it cleared) needs no other read.
public struct PluginIssue: Codable, Equatable, Sendable {
    /// dataCritical or dataWarning.
    public let severity: String
    public let pluginName: String
    public let title: String
    public let detail: String
    public let opens: String

    public init(severity: String, pluginName: String, title: String, detail: String = "", opens: String = "") {
        self.severity = severity
        self.pluginName = pluginName
        self.title = title
        self.detail = detail
        self.opens = opens
    }

    private enum CodingKeys: String, CodingKey { case severity = "s", pluginName = "p", title = "t", detail = "d", opens = "o" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        severity = try c.field(.severity, dataWarning)
        pluginName = try c.field(.pluginName, "")
        title = try c.field(.title, "")
        detail = try c.field(.detail, "")
        opens = try c.field(.opens, "")
    }

    /// From its stored form; a value that is not one reads as a bare severity.
    public init(value: String) {
        if let data = value.data(using: .utf8), let issue = try? JSONDecoder().decode(PluginIssue.self, from: data) {
            self = issue
        } else {
            self.init(severity: value == dataCritical ? dataCritical : dataWarning, pluginName: "", title: "")
        }
    }

    /// The stored form (JSON, sorted keys so that the same issue stores the same string).
    public var value: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return (try? encoder.encode(self)).flatMap { String(data: $0, encoding: .utf8) } ?? severity
    }
}

/// The severity of a stored plugin issue.
public func pluginIssueSeverity(_ value: String) -> String { PluginIssue(value: value).severity }

/// The plugin findings worth a notification, keyed by their `key`: warnings and critical ones
/// (an info is shown, not notified), one per key (the worst). With `checkupWatched`, the
/// findings the checkup reports from a section of its own (`checkupNative`) are left to it. An
/// imported plugin's findings (`user`) are notified only with `importedWatched`: a plugin pasted
/// in does not page anyone by itself.
public func pluginIssuesOf(_ report: PluginFindingsReport, checkupWatched: Bool, importedWatched: Bool = false) -> [String: String] {
    var out: [String: PluginIssue] = [:]
    for finding in report.findings where finding.severity != .info && !(checkupWatched && finding.checkupNative)
        && !(finding.info.user && !importedWatched) {
        let key = finding.info.key
        let severity = finding.severity == .critical ? dataCritical : dataWarning
        if out[key]?.severity == dataCritical { continue }
        out[key] = PluginIssue(severity: severity, pluginName: finding.info.pluginLabel, title: finding.info.headline,
                               detail: finding.info.detail, opens: finding.info.opens)
    }
    return out.mapValues(\.value)
}

/// pluginIssuesOf for a read that may have missed part of the findings: nil when the call
/// failed (the last snapshot's issues stand); otherwise the issues read, plus those `known`
/// (the previous snapshot's) had for a plugin in `errors` (not read in full), or for any plugin
/// when the findings were cut (`truncated`). An unread plugin neither alerts nor clears.
public func pluginIssuesWithGaps(_ report: PluginFindingsReport?, known: [String: String], checkupWatched: Bool,
                                 importedWatched: Bool = false) -> [String: String]? {
    guard let report else { return nil }
    var out = pluginIssuesOf(report, checkupWatched: checkupWatched, importedWatched: importedWatched)
    let unread = Set(report.errors.map(\.plugin))
    for (key, value) in known where out[key] == nil && (report.truncated > 0 || unread.contains(pluginIDOfFindingKey(key))) {
        out[key] = value
    }
    // A finding the checkup reports itself stays with it, even one carried over.
    if checkupWatched {
        let native = Set(report.findings.filter(\.checkupNative).map(\.info.key))
        for key in native { out[key] = nil }
    }
    return out
}

/// The plugin issues a partial read may carry over: the previous snapshot's, only when it is the
/// same cluster and that check watched and read them. Another cluster's issues must never leak in.
public func knownPluginIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.pluginWatched, previous.pluginChecked else { return [:] }
    return previous.pluginIssues
}

/// Plugin findings (see evaluateTrack), keyed by the finding's key ("plugin:<id>:…").
func evaluatePlugins(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]) -> IssueTrack {
    let named = previous?.pluginIssues ?? [:]
    return evaluateTrack(
        previous: previous?.pluginTrack, current: current.pluginTrack, comparable: comparable, severity: pluginIssueSeverity,
        problem: { key, value in
            let issue = PluginIssue(value: value)
            let text = [issue.detail, issue.severity].filter { !$0.isEmpty }.joined(separator: " · ")
            return Alert(key: key, title: "\(issue.pluginName): \(issue.title)", text: text, problem: true)
        },
        cleared: { key in
            let issue = PluginIssue(value: named[key] ?? "")
            return Alert(key: key, title: "\(issue.title) is healthy again", text: issue.pluginName, problem: false)
        },
        alerts: &alerts)
}
