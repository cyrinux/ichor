import Foundation

/// Who is signed in at the Teleport proxy, in KubeSignInInfo.values, for a cluster with no
/// certificate of its own yet (Go teleportUserValue).
public let kubeTeleportUserKey = "teleportUser"

/// One Kubernetes cluster of a Teleport proxy the stored kubeconfig reaches (Go
/// KubeTeleportClusters). Same model on Android.
public struct KubeTeleportCluster: Decodable, Hashable, Sendable {
    public let name: String
    /// The stored context that reaches it.
    public let context: String
    public let signedIn: Bool
    /// The certificate's end, Unix seconds; 0 when not signed in.
    public let expires: Int64

    public init(name: String, context: String, signedIn: Bool = false, expires: Int64 = 0) {
        self.name = name
        self.context = context
        self.signedIn = signedIn
        self.expires = expires
    }

    private enum CodingKeys: String, CodingKey { case name, context, signedIn, expires }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        context = try c.field(.context, "")
        signedIn = try c.field(.signedIn, false)
        expires = try c.field(.expires, 0)
    }
}

/// The Kubernetes clusters of a Teleport proxy, and who is signed in there ("" when nobody).
public struct KubeTeleportClusters: Decodable, Equatable, Sendable {
    public let proxy: String
    public let teleportCluster: String
    public let user: String
    public let clusters: [KubeTeleportCluster]

    private enum CodingKeys: String, CodingKey { case proxy, teleportCluster, user, clusters }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        proxy = try c.field(.proxy, "")
        teleportCluster = try c.field(.teleportCluster, "")
        user = try c.field(.user, "")
        clusters = try c.field(.clusters, [])
    }

    public static func decode(_ json: String) throws -> KubeTeleportClusters {
        try TalosJSON.decode(KubeTeleportClusters.self, from: json)
    }

    /// Whether "Sign out of this Teleport proxy" has anything to remove.
    public var signedInAnywhere: Bool { !user.isEmpty || clusters.contains(where: \.signedIn) }
}

/// A Kubernetes cluster added by name (Go KubeTeleportAddCluster): the kubeconfig to store and
/// its new context, to sign in next.
public struct KubeTeleportAdded: Decodable, Equatable, Sendable {
    public let kubeconfig: String
    public let context: String

    public static func decode(_ json: String) throws -> KubeTeleportAdded {
        try TalosJSON.decode(KubeTeleportAdded.self, from: json)
    }
}

public extension KubeSignInInfo {
    /// Who is signed in at the Teleport proxy when this cluster has no certificate yet: its
    /// sign-in only gets the certificate (the identity provider usually skips the password).
    var teleportUserElsewhere: String? {
        guard isTeleport, !signedIn, let user = values[kubeTeleportUserKey], !user.isEmpty else { return nil }
        return user
    }

    /// The Kubernetes cluster to name on "Get a certificate for …", nil for a plain "Sign in".
    var teleportCertificateTarget: String? {
        guard teleportUserElsewhere != nil, let kube = values["kubeCluster"], !kube.isEmpty else { return nil }
        return kube
    }
}
