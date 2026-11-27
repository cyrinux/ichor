import Foundation

/// A resource type a node serves (ResourceTypes), like a row of `talosctl get rd`.
public struct ResourceType: Decodable, Equatable, Identifiable, Hashable, Sendable {
    /// "MachineConfigs.config.talos.dev"
    public let type: String
    public let aliases: [String]
    public let namespace: String
    /// "" for public data; "sensitive" (or stronger) for resources that hold secrets.
    public let sensitivity: String

    public var id: String { "\(namespace)/\(type)" }

    public init(type: String, aliases: [String] = [], namespace: String = "", sensitivity: String = "") {
        self.type = type
        self.aliases = aliases
        self.namespace = namespace
        self.sensitivity = sensitivity
    }

    private enum CodingKeys: String, CodingKey { case type, aliases, namespace, sensitivity }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        aliases = try c.field(.aliases, [])
        namespace = try c.field(.namespace, "")
        sensitivity = try c.field(.sensitivity, "")
    }

    /// Sensitive resources (secrets, keys): shown with a lock, their YAML hidden until asked for.
    public var isSensitive: Bool { isSensitiveResource(sensitivity) }

    /// "MachineConfigs" of "MachineConfigs.config.talos.dev".
    public var shortName: String {
        type.split(separator: ".", maxSplits: 1).first.map(String.init) ?? type
    }
}

/// Anything but a blank or "non-sensitive" sensitivity (same rule as Android).
public func isSensitiveResource(_ sensitivity: String) -> Bool {
    let value = sensitivity.trimmingCharacters(in: .whitespaces).lowercased()
    return !(value.isEmpty || value == "non-sensitive" || value == "nonsensitive")
}

public struct ResourceItem: Decodable, Equatable, Identifiable, Hashable, Sendable {
    public let id: String
    public let namespace: String
    public let version: String
    public let phase: String
    /// Unix ms, 0 when unknown.
    public let updated: Int64
    /// Talos 1.15 replaced its secret values with REDACTED for this role (below os:admin).
    public let redacted: Bool

    public init(
        id: String, namespace: String = "", version: String = "", phase: String = "", updated: Int64 = 0,
        redacted: Bool = false
    ) {
        self.id = id
        self.namespace = namespace
        self.version = version
        self.phase = phase
        self.updated = updated
        self.redacted = redacted
    }

    private enum CodingKeys: String, CodingKey { case id, namespace, version, phase, updated, redacted }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        namespace = try c.field(.namespace, "")
        // Go sends the version as a string; tolerate a number.
        if let text = try? c.decodeIfPresent(String.self, forKey: .version) {
            version = text
        } else if let number = try? c.decodeIfPresent(Int64.self, forKey: .version) {
            version = "\(number)"
        } else {
            version = ""
        }
        phase = try c.field(.phase, "")
        updated = try c.field(.updated, 0)
        redacted = try c.field(.redacted, false)
    }
}

public struct ResourceItems: Decodable, Equatable, Sendable {
    public let items: [ResourceItem]
    /// Go stopped at its item limit.
    public let truncated: Bool

    public init(items: [ResourceItem], truncated: Bool = false) {
        self.items = items
        self.truncated = truncated
    }

    private enum CodingKeys: String, CodingKey { case items, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.field(.items, [])
        truncated = try c.field(.truncated, false)
    }
}

public struct ResourceDocument: Decodable, Equatable, Sendable {
    public let yaml: String
    /// As `ResourceItem.redacted`; Talos 1.14 and older never send it.
    public let redacted: Bool

    public init(yaml: String, redacted: Bool = false) {
        self.yaml = yaml
        self.redacted = redacted
    }

    private enum CodingKeys: String, CodingKey { case yaml, redacted }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        yaml = try c.field(.yaml, "")
        redacted = try c.field(.redacted, false)
    }
}

/// The placeholder Talos puts in place of each secret value it redacts.
public let redactedPlaceholder = "REDACTED"

/// A run of YAML text; `redacted` when it is the REDACTED placeholder (shown dimmed).
public struct YAMLSegment: Equatable, Sendable {
    public let text: String
    public let redacted: Bool

    public init(text: String, redacted: Bool) {
        self.text = text
        self.redacted = redacted
    }
}

/// `yaml` cut around each whole-word REDACTED placeholder (same rule as Android).
public func redactedSegments(_ yaml: String) -> [YAMLSegment] {
    var segments: [YAMLSegment] = []
    var plainStart = yaml.startIndex
    var searchStart = yaml.startIndex
    while let found = yaml.range(of: redactedPlaceholder, range: searchStart..<yaml.endIndex) {
        searchStart = found.upperBound
        let before = found.lowerBound > yaml.startIndex ? yaml[yaml.index(before: found.lowerBound)] : nil
        let after = found.upperBound < yaml.endIndex ? yaml[found.upperBound] : nil
        guard !isYAMLWordCharacter(before), !isYAMLWordCharacter(after) else { continue }
        if plainStart < found.lowerBound {
            segments.append(YAMLSegment(text: String(yaml[plainStart..<found.lowerBound]), redacted: false))
        }
        segments.append(YAMLSegment(text: redactedPlaceholder, redacted: true))
        plainStart = found.upperBound
    }
    if plainStart < yaml.endIndex {
        segments.append(YAMLSegment(text: String(yaml[plainStart...]), redacted: false))
    }
    return segments
}

private func isYAMLWordCharacter(_ character: Character?) -> Bool {
    guard let character else { return false }
    return character.isLetter || character.isNumber || character == "_" || character == "-"
}

/// The types of one namespace, by name.
public struct ResourceTypeGroup: Equatable, Identifiable, Sendable {
    public let namespace: String
    public let types: [ResourceType]

    public var id: String { namespace }

    public init(namespace: String, types: [ResourceType]) {
        self.namespace = namespace
        self.types = types
    }
}

/// Types whose name or one alias contains `query` (case-insensitive; all when blank),
/// grouped by namespace, namespaces and types sorted by name.
public func groupResourceTypes(_ types: [ResourceType], query: String = "") -> [ResourceTypeGroup] {
    let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
    let shown =
        needle.isEmpty
        ? types
        : types.filter { type in
            type.type.lowercased().contains(needle) || type.aliases.contains { $0.lowercased().contains(needle) }
        }
    return Dictionary(grouping: shown, by: \.namespace)
        .map {
            ResourceTypeGroup(namespace: $0.key, types: $0.value.sorted { $0.type.lowercased() < $1.type.lowercased() })
        }
        .sorted { $0.namespace < $1.namespace }
}

/// Items whose id contains `query` (case-insensitive; all when blank), in Go's order.
public func filterResourceItems(_ items: [ResourceItem], query: String) -> [ResourceItem] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    return needle.isEmpty ? items : items.filter { $0.id.range(of: needle, options: .caseInsensitive) != nil }
}
