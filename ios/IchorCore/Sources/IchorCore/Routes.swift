import Foundation

// Mirrors go/ichorgo/kube_routes.go.

public struct KubeRouteList: Decodable, Equatable, Sendable {
    public let routes: [KubeRoute]

    public init(routes: [KubeRoute] = []) { self.routes = routes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        routes = try c.field(.routes, [])
    }

    private enum CodingKeys: String, CodingKey { case routes }
}

/// A URL an app is served at: a host of an Ingress, a Gateway API HTTPRoute, GRPCRoute or
/// TLSRoute, or a Traefik IngressRoute(TCP) whose backend selects its pods.
public struct KubeRoute: Decodable, Hashable, Identifiable, Sendable {
    /// Ingress, HTTPRoute, GRPCRoute, TLSRoute, IngressRoute or IngressRouteTCP, shown as is.
    public let kind: String
    public let namespace: String
    public let name: String
    public let url: String
    /// The backend Service, in namespace.
    public let service: String
    /// The API group the kind is read at ("" for the core group).
    public let group: String
    public let version: String
    public let resource: String
    /// The certificate the route's host serves, read from a TLS handshake: its expiry (unix
    /// seconds), 0 when unknown; what is wrong with it (RouteCertIssue), "" when fine or unknown;
    /// the TLS Secret the route names; whether cert-manager writes it.
    public let certNotAfter: Int64
    public let certIssue: String
    public let certSecret: String
    public let certManaged: Bool

    public var id: String { url }

    /// Whole days until the certificate expires, rounded towards the past.
    public func certDaysLeft(now: Int64) -> Int64 {
        let seconds = certNotAfter - now
        return seconds >= 0 ? seconds / 86_400 : -((-seconds + 86_399) / 86_400)
    }

    /// How bad the certificate is: nil when it is fine or unknown.
    public var certHealth: ServiceHealth? {
        switch certIssue {
        case RouteCertIssue.expiring, RouteCertIssue.hostMismatch: .warning
        case RouteCertIssue.expired: .critical
        default: nil
        }
    }

    /// Where the route itself is read, so its object summary opens: nil when the answer
    /// carries no resource (an older ichorgo).
    public var apiResource: KubeAPIResource? {
        guard !resource.isEmpty else { return nil }
        return KubeAPIResource(group: group, version: version.isEmpty ? "v1" : version, resource: resource, kind: kind)
    }

    /// The URL without its scheme, to show.
    public var label: String {
        guard let range = url.range(of: "://") else { return url }
        return String(url[range.upperBound...])
    }

    public init(kind: String, namespace: String, name: String, url: String, service: String = "",
                group: String = "", version: String = "", resource: String = "",
                certNotAfter: Int64 = 0, certIssue: String = "", certSecret: String = "", certManaged: Bool = false) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.url = url
        self.service = service
        self.group = group
        self.version = version
        self.resource = resource
        self.certNotAfter = certNotAfter
        self.certIssue = certIssue
        self.certSecret = certSecret
        self.certManaged = certManaged
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decode(String.self, forKey: .kind)
        namespace = try c.decode(String.self, forKey: .namespace)
        name = try c.decode(String.self, forKey: .name)
        url = try c.decode(String.self, forKey: .url)
        service = try c.field(.service, "")
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        certNotAfter = try c.field(.certNotAfter, 0)
        certIssue = try c.field(.certIssue, "")
        certSecret = try c.field(.certSecret, "")
        certManaged = try c.field(.certManaged, false)
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, url, service, group, version, resource, certNotAfter, certIssue, certSecret, certManaged
    }
}

/// What is wrong with the certificate a route serves (KubeRoute.certIssue).
public enum RouteCertIssue {
    public static let expired = "expired"
    public static let expiring = "expiring"
    public static let hostMismatch = "hostMismatch"
}

/// One pod of an app, as KubeAppRoutes takes it.
public struct RoutePod: Encodable, Hashable, Sendable {
    public let namespace: String
    public let pod: String

    public init(namespace: String, pod: String) {
        self.namespace = namespace
        self.pod = pod
    }
}

extension InventoryApp {
    /// Its pods once each (a pod is listed per node it was seen on), in order.
    public var routePods: [RoutePod] {
        var seen = Set<RoutePod>()
        return pods.map { RoutePod(namespace: $0.namespace, pod: $0.pod) }.filter { seen.insert($0).inserted }
    }
}
