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

/// A URL an app is served at: a host of an Ingress or HTTPRoute whose backend selects its pods.
public struct KubeRoute: Decodable, Equatable, Identifiable, Sendable {
    /// Ingress or HTTPRoute.
    public let kind: String
    public let namespace: String
    public let name: String
    public let url: String
    /// The backend Service, in namespace.
    public let service: String

    public var id: String { url }

    /// The URL without its scheme, to show.
    public var label: String {
        guard let range = url.range(of: "://") else { return url }
        return String(url[range.upperBound...])
    }

    public init(kind: String, namespace: String, name: String, url: String, service: String = "") {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.url = url
        self.service = service
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decode(String.self, forKey: .kind)
        namespace = try c.decode(String.self, forKey: .namespace)
        name = try c.decode(String.self, forKey: .name)
        url = try c.decode(String.self, forKey: .url)
        service = try c.field(.service, "")
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, url, service }
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
