import Foundation

// The tags the user gives clusters ("prod", "eu"): by context fingerprint, only on this device
// and in backups, never in the config. Same rules as Android (model/ClusterTags.kt).

/// Longest tag: it sits on a chip.
public let clusterTagMax = 24

/// Most tags on one cluster.
public let clusterTagsMax = 8

/// A line typed for a cluster's tags, split on commas and spaces: lowercase, capped, no empties or repeats.
public func normalizeTags(_ input: String) -> [String] {
    var out: [String] = []
    for part in input.split(whereSeparator: { $0 == "," || $0.isWhitespace }) {
        let tag = String(part.lowercased().prefix(clusterTagMax))
        if !tag.isEmpty, !out.contains(tag) { out.append(tag) }
    }
    return Array(out.prefix(clusterTagsMax))
}

/// The tags of `saved` (by fingerprint) whose cluster is still among `fingerprints`.
public func keepClusterTags(saved: [String: [String]], fingerprints: [String]) -> [String: [String]] {
    let known = Set(fingerprints.filter { !$0.isEmpty })
    return saved.filter { known.contains($0.key) }
}

/// The tags of a cluster whose contexts are `fingerprints`: a context added later has none of its own yet.
public func clusterTagsOf(saved: [String: [String]], fingerprints: [String]) -> [String] {
    var out: [String] = []
    for tag in fingerprints.flatMap({ saved[$0] ?? [] }) where !out.contains(tag) { out.append(tag) }
    return out
}

/// The fingerprints of every context of `context`'s cluster: a per-cluster setting covers them all.
public func clusterFingerprints(of context: ContextSummary, contexts: [ContextSummary]) -> [String] {
    let key = monitorClusterKey(context)
    let all = contexts.filter { monitorClusterKey($0) == key }.map(\.fingerprint) + [context.fingerprint]
    var out: [String] = []
    for fingerprint in all where !fingerprint.isEmpty && !out.contains(fingerprint) { out.append(fingerprint) }
    return out
}

/// The tags of the clusters other than `own` (its fingerprints), minus those already in `typed`, sorted.
public func tagSuggestions(saved: [String: [String]], own: [String], typed: String) -> [String] {
    let present = Set(normalizeTags(typed))
    let others = saved.filter { !own.contains($0.key) }.values.flatMap { $0 }
    return Set(others).subtracting(present).sorted()
}

/// What of `selected` still means something for `groups` ("" is the untagged clusters): a stale tag
/// never hides the fleet.
public func activeTagFilter(groups: [FleetGroup], selected: Set<String>) -> Set<String> {
    selected.intersection(groups.map(\.tag))
}

/// The ranked `rows` in any group of `selected`; all of them when none of it applies.
public func fleetRowsWithTags(_ rows: [FleetRow], groups: [FleetGroup], selected: Set<String>) -> [FleetRow] {
    let active = activeTagFilter(groups: groups, selected: selected)
    guard !active.isEmpty else { return rows }
    let keys = Set(groups.filter { active.contains($0.tag) }.flatMap(\.keys))
    return rows.filter { keys.contains($0.key) }
}

/// The fleet filter as `@AppStorage` keeps it: a JSON array, since "" (untagged) is a value of its own.
public func encodeTagFilter(_ selected: Set<String>) -> String {
    (try? JSONEncoder().encode(selected.sorted())).map { String(decoding: $0, as: UTF8.self) } ?? "[]"
}

/// `encodeTagFilter` read back; anything else is no filter.
public func decodeTagFilter(_ stored: String) -> Set<String> {
    Set((try? JSONDecoder().decode([String].self, from: Data(stored.utf8))) ?? [])
}
