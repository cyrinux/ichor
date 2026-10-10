import Foundation

// The cluster's Kubernetes events kept live (StartKubeEvents). Mirrors
// go/ichorgo/kube_events_live.go and kube_events_coalesce.go; same rules as Android.

/// The object an event row is about, with the resource Go found for its kind.
public struct LiveKubeEventObject: Codable, Hashable, Sendable {
    public let kind: String
    public let apiVersion: String
    /// "" for the core group.
    public let group: String
    public let version: String
    /// The plural name in API paths; "" when discovery does not know the kind.
    public let resource: String
    public let namespaced: Bool
    public let namespace: String
    public let name: String

    public init(kind: String, apiVersion: String = "v1", group: String = "", version: String = "v1", resource: String = "",
                namespaced: Bool = true, namespace: String = "", name: String) {
        self.kind = kind
        self.apiVersion = apiVersion
        self.group = group
        self.version = version
        self.resource = resource
        self.namespaced = namespaced
        self.namespace = namespace
        self.name = name
    }

    private enum CodingKeys: String, CodingKey { case kind, apiVersion, group, version, resource, namespaced, namespace, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        apiVersion = try c.field(.apiVersion, "")
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        namespaced = try c.field(.namespaced, false)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    /// The namespace to open it in: "" for a cluster-scoped object.
    public var objectNamespace: String { namespaced ? namespace : "" }

    /// The resource to open its summary with; nil when Go could not resolve its kind.
    public var apiResource: KubeAPIResource? {
        guard !resource.isEmpty, !version.isEmpty, !name.isEmpty else { return nil }
        return KubeAPIResource(group: group, version: version, resource: resource, kind: kind, namespaced: namespaced)
    }

    /// "Pod shop/web-1", or "Node worker-1" for a cluster-scoped one.
    public var label: String {
        let path = objectNamespace.isEmpty ? name : "\(objectNamespace)/\(name)"
        return kind.isEmpty ? path : "\(kind) \(path)"
    }
}

/// Events coalesced by object, reason and type: the latest note, the summed count, first and
/// last seen.
public struct LiveKubeEvent: Codable, Equatable, Identifiable, Sendable {
    /// Stable for the row's life.
    public let key: String
    /// Normal | Warning
    public let type: String
    public let reason: String
    public let note: String
    public let regarding: LiveKubeEventObject
    public let count: Int
    /// Unix milliseconds.
    public let firstSeen: Int64
    /// Unix milliseconds.
    public let lastSeen: Int64
    /// The component that reported it; "" when none.
    public let source: String

    public var id: String { key }
    public var isWarning: Bool { type == "Warning" }

    public init(key: String, type: String = "Normal", reason: String, note: String = "", regarding: LiveKubeEventObject,
                count: Int = 1, firstSeen: Int64 = 0, lastSeen: Int64, source: String = "") {
        self.key = key
        self.type = type
        self.reason = reason
        self.note = note
        self.regarding = regarding
        self.count = count
        self.firstSeen = firstSeen
        self.lastSeen = lastSeen
        self.source = source
    }

    private enum CodingKeys: String, CodingKey { case key, type, reason, note, regarding, count, firstSeen, lastSeen, source }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.decode(String.self, forKey: .key)
        type = try c.field(.type, "Normal")
        reason = try c.field(.reason, "")
        note = try c.field(.note, "")
        regarding = try c.field(.regarding, LiveKubeEventObject(kind: "", apiVersion: "", version: "", name: ""))
        count = try c.field(.count, 1)
        firstSeen = try c.field(.firstSeen, 0)
        lastSeen = try c.field(.lastSeen, 0)
        source = try c.field(.source, "")
    }

    /// Whether `query` (case-insensitive) is in its reason, object or note; an empty query
    /// matches everything.
    public func matches(_ query: String) -> Bool {
        let needle = query.trimmingCharacters(in: .whitespaces)
        guard !needle.isEmpty else { return true }
        return [reason, regarding.label, note].contains { $0.localizedCaseInsensitiveContains(needle) }
    }
}

/// What one OnEvents call carries: apply `reset`, then `removed`, then `upserts`.
public struct LiveKubeEventsBatch: Codable, Equatable, Sendable {
    public let reset: Bool
    public let upserts: [LiveKubeEvent]
    public let removed: [String]

    public init(reset: Bool = false, upserts: [LiveKubeEvent] = [], removed: [String] = []) {
        self.reset = reset
        self.upserts = upserts
        self.removed = removed
    }

    private enum CodingKeys: String, CodingKey { case reset, upserts, removed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        reset = try c.field(.reset, false)
        upserts = try c.field(.upserts, [])
        removed = try c.field(.removed, [])
    }

    /// One batch doing what `self` then `next` do.
    public func merged(with next: LiveKubeEventsBatch) -> LiveKubeEventsBatch {
        if next.reset { return next }
        let dropped = Set(next.removed)
        let replaced = Set(next.upserts.map(\.key))
        let kept = upserts.filter { !dropped.contains($0.key) && !replaced.contains($0.key) }
        let removedKeys = removed + next.removed.filter { !removed.contains($0) }
        return LiveKubeEventsBatch(reset: reset, upserts: kept + next.upserts, removed: removedKeys)
    }
}

/// How the stream is doing (OnStatus).
public enum LiveKubeEventsState: String, Sendable, WireEnum {
    case live, reconnecting, relisting, polling

    public static let wireFallback = LiveKubeEventsState.live
}

public struct LiveKubeEventsStatus: Codable, Equatable, Sendable {
    public let state: String
    /// Why it reconnects, relists or polls.
    public let reason: String
    /// events.k8s.io/v1, or v1 when the cluster does not serve it.
    public let api: String
    public let tracked: Int
    public let limit: Int
    public let dropped: Int
    /// The first list stopped before reading every event.
    public let partial: Bool
    /// One sentence when partial or rows were let go, else "".
    public let note: String

    public var liveState: LiveKubeEventsState { LiveKubeEventsState(wire: state) }

    public init(state: String = "live", reason: String = "", api: String = "", tracked: Int = 0, limit: Int = 0,
                dropped: Int = 0, partial: Bool = false, note: String = "") {
        self.state = state
        self.reason = reason
        self.api = api
        self.tracked = tracked
        self.limit = limit
        self.dropped = dropped
        self.partial = partial
        self.note = note
    }

    private enum CodingKeys: String, CodingKey { case state, reason, api, tracked, limit, dropped, partial, note }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        state = try c.field(.state, "live")
        reason = try c.field(.reason, "")
        api = try c.field(.api, "")
        tracked = try c.field(.tracked, 0)
        limit = try c.field(.limit, 0)
        dropped = try c.field(.dropped, 0)
        partial = try c.field(.partial, false)
        note = try c.field(.note, "")
    }
}

/// Rows Go keeps at most: the app keeps the same cap.
public let liveKubeEventsCap = 2000

/// `rows` with `batch` applied: cleared on reset, `removed` dropped, `upserts` replacing the
/// row of their key or added; newest lastSeen first (by key on a tie), at most `cap`.
public func applyKubeEventsBatch(_ batch: LiveKubeEventsBatch, to rows: [LiveKubeEvent],
                                 cap: Int = liveKubeEventsCap) -> [LiveKubeEvent] {
    let base = batch.reset ? [] : rows
    let dropped = Set(batch.removed)
    var byKey: [String: LiveKubeEvent] = [:]
    for row in base where !dropped.contains(row.key) { byKey[row.key] = row }
    for row in batch.upserts { byKey[row.key] = row }
    let sorted = byKey.values.sorted { a, b in
        a.lastSeen != b.lastSeen ? a.lastSeen > b.lastSeen : a.key < b.key
    }
    return Array(sorted.prefix(max(0, cap)))
}

/// The rows on screen and, while paused, what came in since: one batch doing it all, applied
/// on resume.
public struct KubeEventsFeed: Equatable, Sendable {
    public let rows: [LiveKubeEvent]
    public let paused: Bool
    public let pending: LiveKubeEventsBatch?
    /// Whether the first batch came in.
    public let loaded: Bool

    public init(rows: [LiveKubeEvent] = [], paused: Bool = false, pending: LiveKubeEventsBatch? = nil, loaded: Bool = false) {
        self.rows = rows
        self.paused = paused
        self.pending = pending
        self.loaded = loaded
    }

    /// How many rows are new or changed in what is held back ("N new").
    public var pendingCount: Int { pending?.upserts.count ?? 0 }

    /// The feed with `batch` shown, or held back while paused (the first batch always shows).
    public func receiving(_ batch: LiveKubeEventsBatch) -> KubeEventsFeed {
        guard paused, loaded else {
            return KubeEventsFeed(rows: applyKubeEventsBatch(batch, to: rows), paused: paused, pending: nil, loaded: true)
        }
        let held = pending.map { $0.merged(with: batch) } ?? batch
        return KubeEventsFeed(rows: rows, paused: true, pending: held, loaded: true)
    }

    public func pausing() -> KubeEventsFeed {
        KubeEventsFeed(rows: rows, paused: true, pending: pending, loaded: loaded)
    }

    /// The feed live again, with what was held back applied.
    public func resuming() -> KubeEventsFeed {
        let shown = pending.map { applyKubeEventsBatch($0, to: rows) } ?? rows
        return KubeEventsFeed(rows: shown, paused: false, pending: nil, loaded: loaded)
    }
}

/// The rows as text to share, newest first: time (UTC), type, reason, object, count, then the
/// note indented.
public func kubeEventsShareText(_ rows: [LiveKubeEvent], header: String = "") -> String {
    let formatter = ISO8601DateFormatter()
    formatter.timeZone = TimeZone(identifier: "UTC")
    let lines = rows.map { row -> String in
        let time = formatter.string(from: Date(timeIntervalSince1970: TimeInterval(row.lastSeen) / 1000))
        var line = "\(time) \(row.type) \(row.reason) \(row.regarding.label)"
        if row.count > 1 { line += " ×\(row.count)" }
        if !row.note.isEmpty { line += "\n    \(row.note)" }
        return line
    }
    let body = lines.joined(separator: "\n")
    return header.isEmpty ? body : "\(header)\n\n\(body)"
}
