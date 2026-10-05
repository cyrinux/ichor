import Foundation

/// The operators a cluster runs that Ichor does not show yet (KubeIntegrations): API groups
/// by operator, plus the groups served that Ichor already reads.
public struct IntegrationReport: Decodable, Equatable, Sendable {
    /// By id; groups by name.
    public let families: [IntegrationFamily]
    public let supported: [String]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        families = try c.field(.families, [])
        supported = try c.field(.supported, [])
    }

    private enum CodingKeys: String, CodingKey { case families, supported }
}

/// One operator ("istio.io") and its API groups.
public struct IntegrationFamily: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let groups: [IntegrationGroup]

    public init(id: String, groups: [IntegrationGroup]) {
        self.id = id
        self.groups = groups
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        groups = try c.field(.groups, [])
    }

    private enum CodingKeys: String, CodingKey { case id, groups }

    /// The family with only the groups named in picked, for a request.
    public func picking(_ picked: Set<String>) -> IntegrationFamily {
        IntegrationFamily(id: id, groups: groups.filter { picked.contains($0.name) })
    }

    /// The JSON IntegrationIssueURL reads, keys sorted.
    public var json: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return (try? encoder.encode(self)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
    }
}

public struct IntegrationGroup: Codable, Equatable, Identifiable, Sendable {
    public let name: String
    public let version: String
    /// Sorted; empty when the group's discovery did not answer.
    public let kinds: [String]

    public var id: String { name }

    public init(name: String, version: String, kinds: [String]) {
        self.name = name
        self.version = version
        self.kinds = kinds
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        version = try c.field(.version, "")
        kinds = try c.field(.kinds, [])
    }

    private enum CodingKeys: String, CodingKey { case name, version, kinds }
}
