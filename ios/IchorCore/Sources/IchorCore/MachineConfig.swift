import Foundation

/// Whether the Talos config schema of a node's version is ready (MachineConfigSchemaPrepare).
public struct ConfigSchemaStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let available: Bool
    /// memory, disk or network.
    public let source: String?
    /// Why the schema is not available.
    public let reason: String?

    public init(version: String, available: Bool, source: String? = nil, reason: String? = nil) {
        self.version = version
        self.available = available
        self.source = source
        self.reason = reason
    }
}

/// Why a draft is not valid YAML (line: 1-based, 0 when unknown).
public struct ConfigSyntaxError: Decodable, Equatable, Sendable {
    public let line: Int
    public let message: String

    public init(line: Int, message: String) {
        self.line = line
        self.message = message
    }
}

/// A field the schema knows that an object does not set yet.
public struct ConfigAddable: Decodable, Equatable, Identifiable, Sendable {
    public let key: String
    public let type: String
    public let description: String?
    public let `enum`: [String]?

    public var id: String { key }

    public init(key: String, type: String, description: String? = nil, enum: [String]? = nil) {
        self.key = key
        self.type = type
        self.description = description
        self.enum = `enum`
    }
}

/// One value of a machine config document, with what the schema tells about it.
public struct ConfigNode: Decodable, Equatable, Identifiable, Sendable {
    public let key: String
    /// Keys and list indexes from the document root.
    public let path: [String]
    /// object, array, string, integer, number, boolean or null.
    public let type: String
    /// Scalars only.
    public let value: String?
    public let title: String?
    public let description: String?
    public let `enum`: [String]?
    /// A hidden secret: not editable.
    public let redacted: Bool?
    public let children: [ConfigNode]?
    public let addable: [ConfigAddable]?
    /// The type of the values an object takes under any key (nil when it only takes its
    /// declared fields).
    public let freeKeyType: String?
    /// The type of a list's items.
    public let itemType: String?

    public var id: String { path.joined(separator: "\u{1F}") }
    public var isContainer: Bool { type == "object" || type == "array" }
    public var isRedacted: Bool { redacted == true }

    public init(key: String, path: [String], type: String, value: String? = nil, title: String? = nil,
                description: String? = nil, enum: [String]? = nil, redacted: Bool? = nil, children: [ConfigNode]? = nil,
                addable: [ConfigAddable]? = nil, freeKeyType: String? = nil, itemType: String? = nil) {
        self.key = key
        self.path = path
        self.type = type
        self.value = value
        self.title = title
        self.description = description
        self.enum = `enum`
        self.redacted = redacted
        self.children = children
        self.addable = addable
        self.freeKeyType = freeKeyType
        self.itemType = itemType
    }

    private enum CodingKeys: String, CodingKey {
        case key, path, type, value, title, description, `enum`, redacted, children, addable, freeKeyType, itemType
    }

    // Go encodes an empty path (a document root) as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        path = try c.field(.path, [])
        type = try c.field(.type, "")
        value = try c.decodeIfPresent(String.self, forKey: .value)
        title = try c.decodeIfPresent(String.self, forKey: .title)
        description = try c.decodeIfPresent(String.self, forKey: .description)
        `enum` = try c.decodeIfPresent([String].self, forKey: .enum)
        redacted = try c.decodeIfPresent(Bool.self, forKey: .redacted)
        children = try c.decodeIfPresent([ConfigNode].self, forKey: .children)
        addable = try c.decodeIfPresent([ConfigAddable].self, forKey: .addable)
        freeKeyType = try c.decodeIfPresent(String.self, forKey: .freeKeyType)
        itemType = try c.decodeIfPresent(String.self, forKey: .itemType)
    }
}

/// One YAML document of a machine config (v1alpha1, or a named config document).
public struct ConfigDocument: Decodable, Equatable, Identifiable, Sendable {
    public let index: Int
    public let title: String
    public let node: ConfigNode

    public var id: Int { index }

    public init(index: Int, title: String, node: ConfigNode) {
        self.index = index
        self.title = title
        self.node = node
    }
}

/// A machine config draft as a tree (MachineConfigDescribe).
public struct ConfigTree: Decodable, Equatable, Sendable {
    /// Described with the Talos schema.
    public let schema: Bool
    /// Empty when `error` is set.
    public let documents: [ConfigDocument]
    public let error: ConfigSyntaxError?

    public init(schema: Bool, documents: [ConfigDocument], error: ConfigSyntaxError? = nil) {
        self.schema = schema
        self.documents = documents
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case schema, documents, error }

    // Go encodes empty slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        schema = try c.field(.schema, false)
        documents = try c.field(.documents, [])
        error = try c.decodeIfPresent(ConfigSyntaxError.self, forKey: .error)
    }
}

/// One change to a draft (MachineConfigEdit): `doc` and `path` as the tree gave them.
public struct ConfigEdit: Encodable, Equatable, Sendable {
    public let doc: Int
    public let path: [String]
    /// set, add or remove.
    public let op: String
    public let key: String
    public let type: String
    public let value: String

    /// Replaces the scalar at `path`.
    public static func set(doc: Int, path: [String], type: String, value: String) -> ConfigEdit {
        ConfigEdit(doc: doc, path: path, op: "set", key: "", type: type, value: value)
    }

    /// A new `key` in the object at `path`, or (empty `key`) a new item at the end of the list.
    public static func add(doc: Int, path: [String], key: String, type: String, value: String) -> ConfigEdit {
        ConfigEdit(doc: doc, path: path, op: "add", key: key, type: type, value: value)
    }

    public static func remove(doc: Int, path: [String]) -> ConfigEdit {
        ConfigEdit(doc: doc, path: path, op: "remove", key: "", type: "", value: "")
    }

    /// The edit as the JSON the Go core takes.
    public func json() -> String {
        (try? TalosJSON.encode(self)) ?? "{}"
    }
}

/// One line of the diff between the node's config and a draft.
public struct ConfigDiffLine: Decodable, Equatable, Identifiable, Sendable {
    public enum Kind: String, Decodable, Sendable {
        case hunk, context, added, removed

        /// A kind this version does not know reads as context.
        public init(from decoder: Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .context
        }
    }

    public let kind: Kind
    public let text: String
    /// Its position in the diff (the JSON has no id).
    public var id: Int

    public init(id: Int, kind: Kind, text: String) {
        self.id = id
        self.kind = kind
        self.text = text
    }

    private enum CodingKeys: String, CodingKey { case kind, text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, .context)
        text = try c.field(.text, "")
        id = 0
    }
}

/// What applying a draft would change (MachineConfigPreview).
public struct ConfigPreview: Decodable, Equatable, Sendable {
    public let changed: Bool
    public let lines: [ConfigDiffLine]
    /// Talos would reboot the node to apply this, so it cannot be tried.
    public let needsReboot: Bool

    public init(changed: Bool, lines: [ConfigDiffLine], needsReboot: Bool) {
        self.changed = changed
        self.lines = lines
        self.needsReboot = needsReboot
    }

    private enum CodingKeys: String, CodingKey { case changed, lines, needsReboot }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        changed = try c.field(.changed, false)
        lines = (try c.field(.lines, [ConfigDiffLine]())).enumerated().map {
            ConfigDiffLine(id: $0.offset, kind: $0.element.kind, text: $0.element.text)
        }
        needsReboot = try c.field(.needsReboot, false)
    }
}

/// A phase change of a config applied in try mode (ConfigTryListener.OnProgress).
public struct ConfigTryProgress: Decodable, Equatable, Sendable {
    public enum Phase: String, Decodable, Sendable {
        case applying, trying, keeping, reverting

        /// A phase this version does not know reads as trying.
        public init(from decoder: Decoder) throws {
            self = Phase(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .trying
        }
    }

    public let phase: Phase
    /// Not empty when keeping or reverting failed and the try goes on.
    public let message: String
    /// When the node reverts by itself, Unix milliseconds; 0 when none.
    public let deadline: Int64
    /// Unix milliseconds.
    public let at: Int64

    public var deadlineDate: Date? {
        deadline == 0 ? nil : Date(timeIntervalSince1970: Double(deadline) / 1000)
    }

    public init(phase: Phase, message: String = "", deadline: Int64 = 0, at: Int64 = 0) {
        self.phase = phase
        self.message = message
        self.deadline = deadline
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case phase, message, deadline, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, .trying)
        message = try c.field(.message, "")
        deadline = try c.field(.deadline, 0)
        at = try c.field(.at, 0)
    }
}

/// How a try ended: the config was kept, the previous one is back, or the run failed.
public enum ConfigTryOutcome: Equatable, Sendable {
    case kept
    case reverted
    case failed(String)
}

public enum ConfigTryEvent: Equatable, Sendable {
    case progress(ConfigTryProgress)
    case done(ConfigTryOutcome)
}

/// The timeouts a try can run with, in seconds (the ones StartConfigTry takes).
public let configTryTimeouts: [Int] = [60, 300, 600]

/// ConfigTryListener.OnDone as an outcome: "kept" or "reverted", anything else is a failure.
public func configTryOutcome(outcome: String, errMessage: String) -> ConfigTryOutcome {
    guard errMessage.isEmpty else { return .failed(errMessage) }
    switch outcome {
    case "kept": return .kept
    case "reverted": return .reverted
    default: return .failed(outcome)
    }
}

private let configConcreteTypes = ["string", "integer", "number", "boolean", "object", "array"]

/// The types offered for a new value of schema type `type`: itself when it names one, any of
/// them when the schema does not tell ("any", "").
public func configValueTypes(for type: String) -> [String] {
    configConcreteTypes.contains(type) ? [type] : configConcreteTypes
}

/// Whole seconds until the node reverts, rounded up, never below 0.
public func configSecondsLeft(deadline: Date, now: Date) -> Int {
    max(0, Int(deadline.timeIntervalSince(now).rounded(.up)))
}
