import Foundation

// Mirrors go/ichorgo/kube_tailscale.go.

/// The Tailscale Kubernetes operator: its Deployment, connectors, proxy groups, proxy classes and
/// the tailnet nameserver, problems first in every list.
public struct Tailscale: Decodable, Equatable, Sendable {
    public let installed: Bool
    /// The served tailscale.com version, "" when not installed.
    public let version: String
    /// A list that failed; what could be read still shows.
    public let error: String
    /// A kind had more objects than the reader keeps.
    public let truncated: Bool
    /// Nil when no operator Deployment was found.
    public let `operator`: TailscaleOperator?
    public let connectors: [TailscaleConnector]
    public let proxyGroups: [TailscaleProxyGroup]
    public let proxyClasses: [TailscaleProxyClass]
    public let dnsConfig: TailscaleDNSConfig?
    public let counts: TailscaleCounts

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
        `operator` = try c.decodeIfPresent(TailscaleOperator.self, forKey: .operator)
        connectors = try c.field(.connectors, [])
        proxyGroups = try c.field(.proxyGroups, [])
        proxyClasses = try c.field(.proxyClasses, [])
        dnsConfig = try c.decodeIfPresent(TailscaleDNSConfig.self, forKey: .dnsConfig)
        counts = try c.field(.counts, TailscaleCounts())
    }

    private enum CodingKeys: String, CodingKey {
        case installed, version, error, truncated, `operator`, connectors, proxyGroups, proxyClasses, dnsConfig, counts
    }

    /// Nothing to list: no connector, proxy group, proxy class or DNS config.
    public var isEmpty: Bool { connectors.isEmpty && proxyGroups.isEmpty && proxyClasses.isEmpty && dnsConfig == nil }

    /// The API version the object summary reads, the reader's own when it named none.
    public var apiVersion: String { version.isEmpty ? TailscaleKind.defaultVersion : version }
}

public struct TailscaleCounts: Decodable, Equatable, Sendable {
    public let ok: Int
    public let warning: Int
    public let critical: Int

    public init(ok: Int = 0, warning: Int = 0, critical: Int = 0) {
        self.ok = ok
        self.warning = warning
        self.critical = critical
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.field(.ok, 0)
        warning = try c.field(.warning, 0)
        critical = try c.field(.critical, 0)
    }

    private enum CodingKeys: String, CodingKey { case ok, warning, critical }
}

/// The operator's Deployment: when it is down, nothing else is reconciled.
public struct TailscaleOperator: Decodable, Equatable, Sendable {
    public let namespace: String
    public let name: String
    public let ready: Int
    public let desired: Int
    public let image: String
    public let health: ServiceHealth

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        ready = try c.field(.ready, 0)
        desired = try c.field(.desired, 0)
        image = try c.field(.image, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, ready, desired, image, health }
}

/// One tailnet device a Connector or ProxyGroup runs.
public struct TailnetDevice: Decodable, Equatable, Sendable {
    public let hostname: String
    public let tailnetIPs: [String]
    public let staticEndpoints: [String]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hostname = try c.field(.hostname, "")
        tailnetIPs = try c.field(.tailnetIPs, [])
        staticEndpoints = try c.field(.staticEndpoints, [])
    }

    private enum CodingKeys: String, CodingKey { case hostname, tailnetIPs, staticEndpoints }
}

/// A row's readiness from its operator condition, and the Go core's grouping of it.
public struct TailscaleState: Decodable, Equatable, Sendable {
    public let ready: Bool
    public let reason: String
    public let message: String
    public let health: ServiceHealth

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ready = try c.field(.ready, false)
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case ready, reason, message, health }
}

/// A subnet router, an exit node or an app connector.
public struct TailscaleConnector: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let hostname: String
    public let tailnetIPs: [String]
    /// status.subnetRoutes as the operator writes it: comma-separated.
    public let subnetRoutes: String
    public let advertisedRoutes: [String]
    public let exitNode: Bool
    public let appConnector: Bool
    public let appConnectorRoutes: [String]
    public let tags: [String]
    public let proxyClass: String
    public let replicas: Int
    public let devices: [TailnetDevice]
    public let state: TailscaleState

    public var id: String { name }

    /// The routes it advertises: the spec's, else what the status says it serves.
    public var routes: [String] {
        if !advertisedRoutes.isEmpty { return advertisedRoutes }
        return subnetRoutes.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    public init(from decoder: Decoder) throws {
        state = try TailscaleState(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        hostname = try c.field(.hostname, "")
        tailnetIPs = try c.field(.tailnetIPs, [])
        subnetRoutes = try c.field(.subnetRoutes, "")
        advertisedRoutes = try c.field(.advertisedRoutes, [])
        exitNode = try c.field(.exitNode, false)
        appConnector = try c.field(.appConnector, false)
        appConnectorRoutes = try c.field(.appConnectorRoutes, [])
        tags = try c.field(.tags, [])
        proxyClass = try c.field(.proxyClass, "")
        replicas = try c.field(.replicas, 0)
        devices = try c.field(.devices, [])
    }

    private enum CodingKeys: String, CodingKey {
        case name, hostname, tailnetIPs, subnetRoutes, advertisedRoutes, exitNode, appConnector, appConnectorRoutes
        case tags, proxyClass, replicas, devices
    }
}

/// A pool of egress, ingress or kube-apiserver proxies.
public struct TailscaleProxyGroup: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// egress, ingress or kube-apiserver.
    public let type: String
    public let replicas: Int
    public let proxyClass: String
    public let tags: [String]
    /// Set for kube-apiserver groups.
    public let url: String
    public let devices: [TailnetDevice]
    /// At least one proxy runs (ready: all of them).
    public let available: Bool
    public let state: TailscaleState

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        state = try TailscaleState(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        replicas = try c.field(.replicas, 0)
        proxyClass = try c.field(.proxyClass, "")
        tags = try c.field(.tags, [])
        url = try c.field(.url, "")
        devices = try c.field(.devices, [])
        available = try c.field(.available, false)
    }

    private enum CodingKeys: String, CodingKey { case name, type, replicas, proxyClass, tags, url, devices, available }
}

/// A ProxyClass, and how many Connectors and ProxyGroups name it.
public struct TailscaleProxyClass: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let usedBy: Int
    public let state: TailscaleState

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        state = try TailscaleState(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        usedBy = try c.field(.usedBy, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, usedBy }
}

/// The DNSConfig: the nameserver that resolves tailnet names in the cluster.
public struct TailscaleDNSConfig: Decodable, Equatable, Sendable {
    public let name: String
    public let nameserverIP: String
    public let state: TailscaleState

    public init(from decoder: Decoder) throws {
        state = try TailscaleState(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        nameserverIP = try c.field(.nameserverIP, "")
    }

    private enum CodingKeys: String, CodingKey { case name, nameserverIP }
}

/// The tailscale.com kinds the screen lists, with the resource the object summary reads.
public enum TailscaleKind: String, Sendable, CaseIterable {
    case connector, proxyGroup, proxyClass, dnsConfig

    public static let group = "tailscale.com"
    public static let defaultVersion = "v1alpha1"

    public var resource: String {
        switch self {
        case .connector: "connectors"
        case .proxyGroup: "proxygroups"
        case .proxyClass: "proxyclasses"
        case .dnsConfig: "dnsconfigs"
        }
    }

    public var kind: String {
        switch self {
        case .connector: "Connector"
        case .proxyGroup: "ProxyGroup"
        case .proxyClass: "ProxyClass"
        case .dnsConfig: "DNSConfig"
        }
    }
}
