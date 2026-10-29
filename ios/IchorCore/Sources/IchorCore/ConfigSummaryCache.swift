import Foundation

/// The summaries of both stored configs, as the app shows them.
public struct StoredSummaries: Codable, Equatable, Sendable {
    public let talos: ConfigSummary?
    public let kube: ConfigSummary?

    public init(talos: ConfigSummary?, kube: ConfigSummary?) {
        self.talos = talos
        self.kube = kube
    }

    public func summary(_ kind: StoredConfigKind) -> ConfigSummary? {
        switch kind {
        case .talosconfig: talos
        case .kubeconfig: kube
        }
    }
}

/// The parsed summaries of the stored configs, kept next to them (sealed like them) so that a
/// launch shows the home without parsing every context. Keyed by each text's digest (Go's
/// ConfigDigest, "" for a store with no summary) and by the mask generation (Go's
/// PrivacyGeneration): the summary is the text's alone while the screenshot mode is off, and
/// depends on the mask's mapping, which never outlives a launch, while it is on. A summary
/// carries certNotAfter, so expiry is still computed at read time.
public struct ConfigSummaryRecord: Codable, Equatable, Sendable {
    public let talosDigest: String
    public let kubeDigest: String
    public let maskGeneration: Int
    /// StoredSummaries as JSON.
    public let summaryJSON: String

    public init(talosDigest: String, kubeDigest: String, maskGeneration: Int, summaries: StoredSummaries) throws {
        self.talosDigest = talosDigest
        self.kubeDigest = kubeDigest
        self.maskGeneration = maskGeneration
        summaryJSON = try TalosJSON.encode(summaries)
    }

    /// The record stored as `data`; nil when it does not decode (a corrupt cache is a miss).
    public static func decode(_ data: Data) -> ConfigSummaryRecord? {
        guard let record = try? JSONDecoder().decode(ConfigSummaryRecord.self, from: data),
            record.summaries != nil
        else { return nil }
        return record
    }

    public func encoded() throws -> Data {
        try JSONEncoder().encode(self)
    }

    public var summaries: StoredSummaries? {
        try? TalosJSON.decode(StoredSummaries.self, from: summaryJSON)
    }

    public func digest(_ kind: StoredConfigKind) -> String {
        switch kind {
        case .talosconfig: talosDigest
        case .kubeconfig: kubeDigest
        }
    }

    /// The digest to hand Go's Parse…IfChanged for `kind`: the cached one when its summary
    /// applies under the mask generation `generation`, "" (always parse) otherwise.
    public func knownDigest(_ kind: StoredConfigKind, generation: Int) -> String {
        guard generation == maskGeneration, summaries?.summary(kind) != nil else { return "" }
        return digest(kind)
    }

    /// Whether to store this record, built from summaries parsed under its maskGeneration, when
    /// the mask is now at `currentGeneration` and `stored` is the record kept: never once the
    /// mask changed in between (real names would be kept as masked ones, or the reverse; the
    /// reparse that follows writes the right ones), and not again when nothing changed.
    public func shouldStore(currentGeneration: Int, stored: ConfigSummaryRecord?) -> Bool {
        maskGeneration == currentGeneration && !holdsSame(as: stored)
    }

    /// Same keys and summaries (compared decoded: the JSON's key order is not stable).
    private func holdsSame(as other: ConfigSummaryRecord?) -> Bool {
        guard let other else { return false }
        return talosDigest == other.talosDigest && kubeDigest == other.kubeDigest
            && maskGeneration == other.maskGeneration && summaries == other.summaries
    }
}

/// The summary of a store from the output of Go's Parse…IfChanged: "" means the digest
/// matched and nothing was parsed, so `record`'s summary is current; anything else is the
/// fresh summary JSON. Throws when "" comes with no cached summary (the caller parses then).
public func summaryFromIfChanged(
    _ output: String, kind: StoredConfigKind,
    record: ConfigSummaryRecord?
) throws -> (summary: ConfigSummary, cached: Bool) {
    if output.isEmpty {
        guard let cached = record?.summaries?.summary(kind) else { throw ConfigSummaryCacheMiss() }
        return (cached, true)
    }
    return (try TalosJSON.decode(ConfigSummary.self, from: output), false)
}

/// Go answered "unchanged" for a summary the record does not hold.
public struct ConfigSummaryCacheMiss: Error, Equatable {
    public init() {}
}
