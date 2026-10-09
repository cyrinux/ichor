import Foundation

/// The Services screen (Go `KubeServices`): each Service with its addresses, ready endpoints
/// and routes, problems first. `partialAccess`: EndpointSlices or routes could not be read.
/// `lbController` names what should give a LoadBalancer Service its address (metallb,
/// cilium), set only when one waits for it.
public struct KubeServices: Decodable, Sendable {
    public let services: [ServiceRow]
    public let partialAccess: Bool
    public let lbController: String

    public var anyPending: Bool { services.contains { $0.pending } }

    private enum CodingKeys: String, CodingKey { case services, partialAccess, lbController }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        services = try c.field(.services, [])
        partialAccess = try c.field(.partialAccess, false)
        lbController = try c.field(.lbController, "")
    }
}

/// What gives LoadBalancer Services their address, as the core names it.
public enum LBController: String, Sendable {
    case metallb, cilium
}

/// A Service. `ports`: kubectl's "80/TCP" ("80:30080/TCP" with a node port). `addresses`: the
/// load balancer's IPs or hostnames and the external IPs. `endpointsKnown`: the slices were read.
public struct ServiceRow: Decodable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let type: String
    public let clusterIP: String
    public let externalName: String
    public let ports: [String]
    public let addresses: [String]
    public let pending: Bool
    public let selector: Bool
    public let endpointsKnown: Bool
    public let endpoints: Int
    public let readyEndpoints: Int
    public let routes: [KubeRoute]
    /// The screens share ok, warning and critical.
    public let level: StorageLevel

    public var id: String { "\(namespace)/\(name)" }
    /// A Service without a cluster IP: its DNS name lists the pods instead.
    public var headless: Bool { clusterIP.caseInsensitiveCompare("None") == .orderedSame }
    /// "2/3", when the endpoints are worth showing: known, and kept by Kubernetes or present.
    public var readyText: String? {
        endpointsKnown && (selector || endpoints > 0) ? "\(readyEndpoints)/\(endpoints)" : nil
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, type, clusterIP, externalName, ports, addresses, pending, selector
        case endpointsKnown, endpoints, readyEndpoints, routes, level
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        clusterIP = try c.field(.clusterIP, "")
        externalName = try c.field(.externalName, "")
        ports = try c.field(.ports, [])
        addresses = try c.field(.addresses, [])
        pending = try c.field(.pending, false)
        selector = try c.field(.selector, false)
        endpointsKnown = try c.field(.endpointsKnown, false)
        endpoints = try c.field(.endpoints, 0)
        readyEndpoints = try c.field(.readyEndpoints, 0)
        routes = try c.field(.routes, [])
        level = try c.wire(.level)
    }
}

/// The Services whose namespace/name, type, an address, a port or a route URL contains `query`.
public func filterServices(_ services: [ServiceRow], query: String) -> [ServiceRow] {
    let q = query.trimmingCharacters(in: .whitespaces)
    guard !q.isEmpty else { return services }
    return services.filter { s in
        ([s.id, s.type, s.clusterIP, s.externalName] + s.addresses + s.ports + s.routes.map(\.url))
            .contains { $0.localizedCaseInsensitiveContains(q) }
    }
}
