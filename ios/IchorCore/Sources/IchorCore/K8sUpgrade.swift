import Foundation

// Mirrors go/ichorgo/upgradek8s.go, upgradek8s_run.go and upgradek8s_versions.go.

/// The versions offered: newer than `from`, at most one minor up, inside the Talos range.
public struct K8sVersionChoice: Decodable, Equatable, Sendable {
    public let from: String
    public let supportedRange: String
    public let lo: Int
    public let hi: Int
    public let versions: [String]
    /// Why `versions` may miss some: the release list could not be read.
    public let warning: String

    public init(from: String, supportedRange: String = "", lo: Int = 0, hi: Int = 0, versions: [String] = [], warning: String = "") {
        self.from = from
        self.supportedRange = supportedRange
        self.lo = lo
        self.hi = hi
        self.versions = versions
        self.warning = warning
    }

    private enum CodingKeys: String, CodingKey { case from, supportedRange, lo, hi, versions, warning }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.field(.from, "")
        supportedRange = try c.field(.supportedRange, "")
        lo = try c.field(.lo, 0)
        hi = try c.field(.hi, 0)
        versions = try c.field(.versions, [])
        warning = try c.field(.warning, "")
    }

    /// `version` (1.X.Y) is one this choice allows: after the current version, at most one
    /// minor up, inside the range.
    public func allows(_ version: String) -> Bool {
        guard let v = Self.parts(version), let f = Self.parts(from), v[0] == 1, f[0] == 1 else { return false }
        if v[1] < f[1] || v[1] > f[1] + 1 { return false }
        if v[1] == f[1] && v[2] <= f[2] { return false }
        if lo > 0 && v[1] < lo { return false }
        if hi > 0 && v[1] > hi { return false }
        return true
    }

    private static func parts(_ version: String) -> [Int]? {
        let p = version.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: "v", with: "").split(separator: ".")
        guard p.count == 3 else { return nil }
        let n = p.compactMap { Int($0) }
        return n.count == 3 ? n : nil
    }
}

/// One component of one node, in the order the run follows.
public struct K8sPlanStep: Decodable, Equatable, Identifiable, Sendable {
    /// controlplane | kubelet
    public let kind: String
    public let node: String
    public let hostname: String
    /// apiserver | controller-manager | scheduler | proxy | kubelet
    public let component: String
    public let image: String
    public let current: String
    public let changed: Bool

    public var id: String { "\(node)/\(component)" }
    public var name: String { hostname.isEmpty ? node : hostname }
    public var controlPlane: Bool { kind == "controlplane" }
    /// The tag the component runs now ("v1.34.0").
    public var currentTag: String { Self.tag(current) }
    public var newTag: String { Self.tag(image) }

    private static func tag(_ image: String) -> String {
        guard let colon = image.lastIndex(of: ":") else { return "" }
        return String(image[image.index(after: colon)...]).components(separatedBy: "@").first ?? ""
    }

    private enum CodingKeys: String, CodingKey { case kind, node, hostname, component, image, current, changed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        component = try c.field(.component, "")
        image = try c.field(.image, "")
        current = try c.field(.current, "")
        changed = try c.field(.changed, false)
    }
}

/// A deprecated API still requested; severity "critical" when the target removes it.
public struct K8sDeprecatedAPI: Decodable, Equatable, Sendable {
    public let api: String
    public let removedIn: String
    public let severity: String

    private enum CodingKeys: String, CodingKey { case api, removedIn, severity }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        api = try c.field(.api, "")
        removedIn = try c.field(.removedIn, "")
        severity = try c.field(.severity, "")
    }
}

public struct K8sUpgradePlan: Decodable, Equatable, Sendable {
    public let from: String
    public let to: String
    public let supportedRange: String
    public let steps: [K8sPlanStep]
    public let deprecatedAPIs: [K8sDeprecatedAPI]
    public let blockers: [String]
    public let warnings: [String]

    public var controlPlaneSteps: [K8sPlanStep] { steps.filter(\.controlPlane) }
    public var kubeletSteps: [K8sPlanStep] { steps.filter { !$0.controlPlane } }
    public var changes: Int { steps.count(where: \.changed) }
    public var canStart: Bool { blockers.isEmpty && changes > 0 }

    private enum CodingKeys: String, CodingKey {
        case from, to, supportedRange, steps, deprecatedAPIs = "deprecatedApis", blockers, warnings
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.field(.from, "")
        to = try c.field(.to, "")
        supportedRange = try c.field(.supportedRange, "")
        steps = try c.field(.steps, [])
        deprecatedAPIs = try c.field(.deprecatedAPIs, [])
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
    }
}

/// A step of the run: the node at `index` among the `total` to change, its phase and components.
public struct K8sUpgradeProgress: Decodable, Equatable, Sendable {
    /// controlplane | kubelet | proxy | done
    public let phase: String
    public let index: Int
    public let total: Int
    public let node: String
    public let hostname: String
    public let component: String
    public let message: String
    public let dryRun: Bool

    public var name: String { hostname.isEmpty ? node : hostname }

    private enum CodingKeys: String, CodingKey { case phase, index, total, node, hostname, component, message, dryRun }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        index = try c.field(.index, 0)
        total = try c.field(.total, 0)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        component = try c.field(.component, "")
        message = try c.field(.message, "")
        dryRun = try c.field(.dryRun, false)
    }
}
