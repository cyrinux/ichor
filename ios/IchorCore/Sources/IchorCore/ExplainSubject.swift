import Foundation

/// What "Explain with AI" is about, as Go's CollectExplanation reads it: `kind` and `json()`.
public enum ExplainSubject: Equatable, Sendable {
    case pod(namespace: String, name: String)
    /// `sourceJSON`: the Alertmanager the Alerts screen reads, as AlertmanagerAlerts takes it.
    case alert(sourceJSON: String, fingerprint: String)
    /// `text` and `fix`: the finding's title and fix as the app words them.
    case finding(CheckupFinding, text: String, fix: String)

    public var kind: String {
        switch self {
        case .pod: "pod"
        case .alert: "alert"
        case .finding: "finding"
        }
    }

    /// The subject as Go reads it.
    public func json() throws -> String {
        switch self {
        case .pod(let namespace, let name):
            try TalosJSON.encode(PodWire(namespace: namespace, name: name))
        case .alert(let sourceJSON, let fingerprint):
            try TalosJSON.encode(AlertWire(source: sourceJSON, fingerprint: fingerprint))
        case .finding(let finding, let text, let fix):
            try TalosJSON.encode(FindingWire(finding: FindingFields(finding), text: text, fix: fix))
        }
    }
}

// The notes "Continue in Diagnosis" writes. English whatever the app's language, like
// healthFailureNote: they are read by the model.

public func podExplainNote(namespace: String, name: String, status: String) -> String {
    "Why is pod \(namespace)/\(name) \(status.isEmpty ? "not running" : status)?"
}

public func alertExplainNote(alertname: String) -> String {
    "Why is the alert \(alertname) firing?"
}

public func findingExplainNote(text: String) -> String {
    "The checkup found: \(text)"
}

private struct PodWire: Encodable {
    let namespace: String
    let name: String
}

private struct AlertWire: Encodable {
    let source: String
    let fingerprint: String
}

private struct FindingWire: Encodable {
    let finding: FindingFields
    let text: String
    let fix: String
}

/// The finding as KubeCheckup gave it, with the plugin's words when it is a plugin's.
private struct FindingFields: Encodable {
    let kind, severity, namespace, name, node, reason, message, extra: String
    let value, limit: Double
    let count: Int
    let since: Int64
    let pluginName, title, detail: String?

    init(_ f: CheckupFinding) {
        kind = f.kindName
        severity = f.severity.rawValue
        namespace = f.namespace
        name = f.name
        node = f.node
        reason = f.reason
        message = f.message
        extra = f.extra
        value = f.value
        limit = f.limit
        count = f.count
        since = f.since
        pluginName = f.plugin?.pluginName
        title = f.plugin?.title
        detail = f.plugin?.detail
    }
}
