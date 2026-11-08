import Foundation

// Mirrors go/ichorgo/omni_allocate.go.

/// How a machine set takes its machines. `type`: static, unlimited or none (machines bound by hand).
public struct OmniAllocation: Decodable, Equatable, Hashable, Sendable {
    public let className: String
    public let type: String
    public let count: Int

    private enum CodingKeys: String, CodingKey { case className, type, count }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        className = try c.field(.className, "")
        type = try c.field(.type, "none")
        count = try c.field(.count, 0)
    }
}

/// A machine set of the cluster. `matchLabels`: what its machine class asks of a machine.
public struct OmniMachineSet: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let role: String
    public let allocation: OmniAllocation
    public let matchLabels: [String]

    public var isControlPlane: Bool { role == "controlplane" }

    private enum CodingKeys: String, CodingKey { case id, role, allocation, matchLabels }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        role = try c.field(.role, "")
        allocation = try c.decode(OmniAllocation.self, forKey: .allocation)
        matchLabels = try c.field(.matchLabels, [])
    }
}

public struct OmniMachineSets: Decodable, Equatable, Sendable {
    public let cluster: String
    public let machineSets: [OmniMachineSet]

    private enum CodingKeys: String, CodingKey { case cluster, machineSets }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cluster = try c.field(.cluster, "")
        machineSets = try c.field(.machineSets, [])
    }
}

/// A machine that registered in Omni and that no cluster has yet.
public struct OmniMachine: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    public let hostname: String
    public let addresses: [String]
    public let talosVersion: String
    public let connected: Bool
    public let maintenance: Bool
    public let labels: [String: String]

    /// What the person reads, and types to confirm.
    public var name: String { hostname.isEmpty ? id : hostname }

    private enum CodingKeys: String, CodingKey {
        case id, hostname, addresses, talosVersion, connected, maintenance, labels
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        hostname = try c.field(.hostname, "")
        addresses = try c.field(.addresses, [])
        talosVersion = try c.field(.talosVersion, "")
        connected = try c.field(.connected, false)
        maintenance = try c.field(.maintenance, false)
        labels = try c.field(.labels, [:])
    }
}

/// What to allocate where.
public struct OmniAllocateSpec: Equatable, Sendable {
    public let machineID: String
    public let machineSetID: String

    public init(machineID: String, machineSetID: String) {
        self.machineID = machineID
        self.machineSetID = machineSetID
    }
}

/// The labels to sign into Omni's join token, as JSON: `role` as ichor/role, then `extra`, typed as
/// key=value pairs separated by commas (a bare key has an empty value; the typed one wins).
public func omniLabelsJSON(role: String, extra: String) -> String {
    var labels = ["ichor/role": role]
    for part in extra.split(separator: ",") {
        let pair = part.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
        let key = pair[0].trimmingCharacters(in: .whitespaces)
        guard !key.isEmpty else { continue }
        labels[key] = pair.count > 1 ? pair[1].trimmingCharacters(in: .whitespaces) : ""
    }
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    return (try? encoder.encode(labels)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
}
