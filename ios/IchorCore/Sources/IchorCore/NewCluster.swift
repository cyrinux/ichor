import Foundation

// Mirrors go/ichorgo/newcluster.go.

/// What the person typed for a new cluster, sent to NewClusterPrepare.
public struct NewClusterSpec: Codable, Equatable, Sendable {
    public let name: String
    /// The machine's address: its only control plane, and the cluster's endpoint.
    public let endpoint: String
    /// Empty: the default the machine's Talos runs.
    public let kubernetesVersion: String
    /// The machine's own Talos version, from its inspection.
    public let talosVersion: String
    public let installDisk: String
    public let allowSchedulingOnControlPlanes: Bool

    public init(
        name: String, endpoint: String, kubernetesVersion: String = "", talosVersion: String = "",
        installDisk: String, allowSchedulingOnControlPlanes: Bool = true
    ) {
        self.name = name
        self.endpoint = endpoint
        self.kubernetesVersion = kubernetesVersion
        self.talosVersion = talosVersion
        self.installDisk = installDisk
        self.allowSchedulingOnControlPlanes = allowSchedulingOnControlPlanes
    }
}

/// A prepared cluster, its secrets held by the Go core behind `draftId`. `talosconfig` is a
/// credential: the app stores it before the run.
public struct NewClusterDraft: Decodable, Equatable, Sendable, CustomStringConvertible {
    public let draftId: String
    public let talosconfig: String
    public let redactedControlplane: String
    public let warnings: [String]
    public let size: Int
    public let talosVersion: String
    public let kubernetesVersion: String

    public var description: String { "NewClusterDraft(\(draftId), talos \(talosVersion), k8s \(kubernetesVersion))" }

    enum CodingKeys: String, CodingKey {
        case draftId, talosconfig, redactedControlplane, warnings, size, talosVersion, kubernetesVersion
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        draftId = try c.decodeIfPresent(String.self, forKey: .draftId) ?? ""
        talosconfig = try c.decodeIfPresent(String.self, forKey: .talosconfig) ?? ""
        redactedControlplane = try c.decodeIfPresent(String.self, forKey: .redactedControlplane) ?? ""
        warnings = try c.decodeIfPresent([String].self, forKey: .warnings) ?? []
        size = try c.decodeIfPresent(Int.self, forKey: .size) ?? 0
        talosVersion = try c.decodeIfPresent(String.self, forKey: .talosVersion) ?? ""
        kubernetesVersion = try c.decodeIfPresent(String.self, forKey: .kubernetesVersion) ?? ""
    }
}

/// A prepared new cluster to run: its draft in the Go core and its `talosconfig`, stored as the
/// context `name` before the run. Kept out of `description`: it is a credential.
public struct NewClusterStart: Equatable, Sendable, CustomStringConvertible {
    public let draftId: String
    public let name: String
    public let talosconfig: String

    public init(draftId: String, name: String, talosconfig: String) {
        self.draftId = draftId
        self.name = name
        self.talosconfig = talosconfig
    }

    public var description: String { "NewClusterStart(\(name))" }
}

/// The demo's maintenance address (go/ichorgo/join_inspect.go): previews work, the run is refused.
public let demoMaintenanceAddress = "demo"
