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

    public var id: String { url }

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
                group: String = "", version: String = "", resource: String = "") {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.url = url
        self.service = service
        self.group = group
        self.version = version
        self.resource = resource
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
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, url, service, group, version, resource }
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
