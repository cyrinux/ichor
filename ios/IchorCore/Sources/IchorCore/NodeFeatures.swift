import Foundation

/// What a node's Talos version can do, by name (Go NodeFeatures). Not the same thing as
/// `Feature`, which is what the talosconfig's role may do.
public enum NodeFeature: String, CaseIterable, Sendable {
    case events, containers, processes, logFollow, serviceControl, packetCapture, upgrade, volumes
    case diskUsage, mounts, kubespan, etcd, etcdSnapshot, etcdMemberActions, resourceBrowser
    case supportBundle, diskHealth, issueConfig, network, connections, time, hardware, images
    case machineConfig, debugShell
}

public struct FeatureSupport: Decodable, Equatable, Sendable {
    public let supported: Bool
    /// "v1.9.0", "" when the feature does not depend on the version.
    public let minVersion: String
    /// Why it is not supported, in Go's words; "" when supported or when minVersion says it all.
    public let reason: String

    public init(supported: Bool, minVersion: String = "", reason: String = "") {
        self.supported = supported
        self.minVersion = minVersion
        self.reason = reason
    }

    private enum CodingKeys: String, CodingKey { case supported, minVersion, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        supported = try c.field(.supported, true)
        minVersion = try c.field(.minVersion, "")
        reason = try c.field(.reason, "")
    }
}

public struct NodeFeatures: Decodable, Equatable, Sendable {
    /// The node's Talos version, "" when unknown.
    public let version: String
    public let features: [String: FeatureSupport]

    public init(version: String = "", features: [String: FeatureSupport] = [:]) {
        self.version = version
        self.features = features
    }

    private enum CodingKeys: String, CodingKey { case version, features }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        features = try c.field(.features, [:])
    }
}

/// Why something is unavailable on a node's Talos version: `minVersion` ("v1.15") when known,
/// "" otherwise. Shown as information, not as a failure.
public struct VersionNotice: Equatable, Sendable {
    public let minVersion: String

    public init(minVersion: String) { self.minVersion = minVersion }
}

/// "1.15" or "V1.15" → "v1.15"; blank stays "".
public func displayTalosVersion(_ version: String) -> String {
    let trimmed = version.trimmingCharacters(in: .whitespaces)
    guard let first = trimmed.first else { return "" }
    if first == "v" { return trimmed }
    if first == "V" { return "v" + trimmed.dropFirst() }
    return "v" + trimmed
}

/// Compares "v1.9" and "1.15.2" numerically, part by part; missing or odd parts count as 0.
/// Negative when `a` is older.
public func compareTalosVersions(_ a: String, _ b: String) -> Int {
    func parts(_ version: String) -> [Int] {
        var text = Substring(version.trimmingCharacters(in: .whitespaces))
        if text.hasPrefix("v") || text.hasPrefix("V") { text = text.dropFirst() }
        return text.split(separator: ".", omittingEmptySubsequences: false).map { Int($0.prefix { $0.isASCII && $0.isNumber }) ?? 0 }
    }
    let pa = parts(a), pb = parts(b)
    for index in 0..<max(pa.count, pb.count) {
        let x = index < pa.count ? pa[index] : 0
        let y = index < pb.count ? pb[index] : 0
        if x != y { return x < y ? -1 : 1 }
    }
    return 0
}

/// Support of `feature` on a node; supported while unknown (features not loaded yet, or a
/// name this core does not report): never block on a guess, the node answers for itself.
/// Same rule as Android.
public func featureSupport(_ features: NodeFeatures?, _ feature: NodeFeature) -> FeatureSupport {
    features?.features[feature.rawValue] ?? FeatureSupport(supported: true)
}

/// Support of a cluster-wide `feature` given the features of the reachable nodes: available
/// as soon as one node has it (the others report their own error inline), and when nothing
/// is known. Unsupported everywhere: the lowest version that would bring it.
public func clusterSupport(_ nodes: [NodeFeatures], _ feature: NodeFeature) -> FeatureSupport {
    let known = nodes.compactMap { $0.features[feature.rawValue] }
    if known.isEmpty || known.contains(where: \.supported) { return FeatureSupport(supported: true) }
    let versioned = known.filter { !$0.minVersion.trimmingCharacters(in: .whitespaces).isEmpty }
    return versioned.min { compareTalosVersions($0.minVersion, $1.minVersion) < 0 } ?? known[0]
}

/// The version in "… needs Talos v1.15 or newer …" or "volumes need Talos v1.8 or newer"
/// (case-insensitive), nil when the text says no such thing.
func neededTalosVersion(_ message: String) -> String? {
    let lower = message.lowercased()
    var search = lower.startIndex..<lower.endIndex
    while let found = lower.range(of: "need", range: search) {
        var rest = lower[found.upperBound...]
        if rest.hasPrefix("s") { rest = rest.dropFirst() }
        guard rest.hasPrefix(" talos ") else {
            search = found.upperBound..<lower.endIndex
            continue
        }
        rest = rest.dropFirst(" talos ".count)
        if rest.hasPrefix("v") { rest = rest.dropFirst() }
        let version = rest.prefix { $0 == "." || ($0.isASCII && $0.isNumber) }
        let parts = version.split(separator: ".", omittingEmptySubsequences: false)
        if (2...3).contains(parts.count), parts.allSatisfy({ !$0.isEmpty }), rest.dropFirst(version.count).hasPrefix(" or newer") {
            return String(version)
        }
        search = found.upperBound..<lower.endIndex
    }
    return nil
}

private let unavailableMarkers = ["not available on this node's talos version", "not available on this talos version"]

/// The notice behind a Go error `message` meaning "this node's Talos version cannot do that"
/// (shown as information, not as a failure), or nil for any other error. Same rule as Android.
public func versionNotice(_ message: String) -> VersionNotice? {
    if let version = neededTalosVersion(message) { return VersionNotice(minVersion: displayTalosVersion(version)) }
    let lower = message.lowercased()
    // gRPC Unimplemented: the node's API has no such method (see friendlyError in the Go core).
    if lower.hasPrefix("unimplemented:") || lower.hasPrefix("unsupported:") || unavailableMarkers.contains(where: lower.contains) {
        return VersionNotice(minVersion: "")
    }
    return nil
}

public extension FeatureSupport {
    /// The notice for an unsupported feature, nil when it is supported. With a reason, the
    /// version is the one it names ("needs Talos vX or newer"), else none: "removed in Talos
    /// vX" must not read as "needs vX". Only without a reason does minVersion speak. Same
    /// rule as Android.
    var notice: VersionNotice? {
        if supported { return nil }
        if reason.trimmingCharacters(in: .whitespaces).isEmpty {
            return VersionNotice(minVersion: displayTalosVersion(minVersion))
        }
        return VersionNotice(minVersion: neededTalosVersion(reason).map(displayTalosVersion) ?? "")
    }
}
