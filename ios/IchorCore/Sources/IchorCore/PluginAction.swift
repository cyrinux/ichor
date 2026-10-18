import Foundation

// A plugin's action buttons (docs/plugins.md "Actions"): the button as the render tree gives it,
// the form of its params, the JSON that crosses to KubePluginAction, and its refusals read back.
// Go checks the params again and decides; this only keeps a form from sending what it would
// refuse anyway, and says the refusal in the app's words.

/// A declared action's button.
public struct PluginActionItem: Decodable, Equatable, Sendable {
    public let id: String
    public let label: String
    public let enabled: Bool
    /// none, plain or typedName.
    public let confirm: String
    public let disabledReason: String
    /// The can-I answer; nil when it is unknown.
    public let allowed: Bool?
    public let params: [PluginParam]

    /// Whether the button may be pressed, as far as the tree says: enabled, and not refused by
    /// the can-I answer (nil, unknown, keeps it on: the API server still decides).
    public var usable: Bool { enabled && allowed != false }

    /// Why the button is off; nil while it is on (an unknown can-I answer claims nothing).
    public var note: PluginActionNote? {
        if !enabled { return disabledReason.isEmpty ? .unavailable : .reason(disabledReason) }
        if allowed == false { return .notAllowed }
        return nil
    }

    /// How the app confirms before running it.
    public var confirmation: PluginActionConfirm { PluginActionConfirm(wire: confirm) }

    /// A param's label, or its name when the action does not declare it.
    public func paramLabel(_ name: String) -> String {
        params.first { $0.name == name }?.label ?? name
    }

    private enum CodingKeys: String, CodingKey { case id, label, enabled, confirm, disabledReason, allowed, params }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        label = try c.field(.label, "")
        // Go always writes it; absent, the button stays on and the server checks enabledWhen.
        enabled = try c.field(.enabled, true)
        confirm = try c.field(.confirm, "")
        disabledReason = try c.field(.disabledReason, "")
        allowed = try c.decodeIfPresent(Bool.self, forKey: .allowed)
        params = try c.field(.params, [])
    }
}

/// What an action button says under it while it is off.
public enum PluginActionNote: Equatable, Sendable {
    /// The plugin's own reason ("not asleep").
    case reason(String)
    /// The credentials may not do it.
    case notAllowed
    /// Off on this object, the plugin says no more.
    case unavailable
}

/// How an action is confirmed: not at all, with a plain question, or by typing the object's name.
public enum PluginActionConfirm: Equatable, Sendable {
    case none, plain, typedName

    /// Go writes "plain" when the plugin says nothing; anything unknown asks too.
    public init(wire: String) {
        switch wire {
        case "none": self = .none
        case "typedName": self = .typedName
        default: self = .plain
        }
    }
}

/// A param's value as it crosses in paramsJSON: a JSON string, integer or boolean.
public enum PluginParamValue: Equatable, Sendable, Encodable {
    case string(String)
    case int(Int64)
    case bool(Bool)

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .string(let s): try c.encode(s)
        case .int(let n): try c.encode(n)
        case .bool(let b): try c.encode(b)
        }
    }

    /// The value as a form field shows it.
    public var text: String {
        switch self {
        case .string(let s): s
        case .int(let n): String(n)
        case .bool(let b): b ? "true" : "false"
        }
    }
}

public struct PluginParam: Decodable, Equatable, Sendable {
    public let name: String
    public let label: String
    /// string, int, bool or enum.
    public let type: String
    public let values: [String]
    /// The plugin's default, read as the param's type; nil when there is none (the param is
    /// then required) or when it does not have the param's type.
    public let defaultValue: PluginParamValue?

    public init(name: String, label: String, type: String, values: [String] = [], defaultValue: PluginParamValue? = nil) {
        self.name = name
        self.label = label
        self.type = type
        self.values = values
        self.defaultValue = defaultValue
    }

    private enum CodingKeys: String, CodingKey { case name, label, type, values, defaultValue = "default" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        label = try c.field(.label, "")
        type = try c.field(.type, "")
        values = try c.field(.values, [])
        defaultValue = Self.decodeDefault(c, type: type)
    }

    /// The default as the type wants it: anything else is no default.
    private static func decodeDefault(_ c: KeyedDecodingContainer<CodingKeys>, type: String) -> PluginParamValue? {
        switch type {
        case "bool": (try? c.decodeIfPresent(Bool.self, forKey: .defaultValue)).flatMap { $0 }.map(PluginParamValue.bool)
        case "int": (try? c.decodeIfPresent(Int64.self, forKey: .defaultValue)).flatMap { $0 }.map(PluginParamValue.int)
        default: (try? c.decodeIfPresent(String.self, forKey: .defaultValue)).flatMap { $0 }.map(PluginParamValue.string)
        }
    }

    /// Whether the call needs a value for it: a param with no default.
    public var required: Bool { defaultValue == nil }
}

/// Why a form field cannot be sent as it is.
public enum PluginParamProblem: Error, Equatable, Sendable {
    /// Empty, with no default to fall back on.
    case required
    /// An int field that does not read as a whole number.
    case notAnInteger
    /// An enum field set to none of its values.
    case notAChoice
    /// A string past pluginParamMaxBytes.
    case tooLong
    /// A type this build does not know: never sent.
    case unsupported
}

/// The longest string param Go accepts, in UTF-8 bytes.
public let pluginParamMaxBytes = 1024

/// The params form of an action: each field's text (string, int and enum params) or switch
/// (bool), starting from the plugin's defaults. Changes return a new form.
public struct PluginActionForm: Equatable, Sendable {
    public let params: [PluginParam]
    private let texts: [String: String]
    private let flags: [String: Bool]

    public init(params: [PluginParam]) {
        self.params = params
        var texts: [String: String] = [:]
        var flags: [String: Bool] = [:]
        for param in params {
            if param.type == "bool" {
                if case .bool(let on) = param.defaultValue { flags[param.name] = on } else { flags[param.name] = false }
            } else {
                texts[param.name] = param.defaultValue?.text ?? ""
            }
        }
        self.texts = texts
        self.flags = flags
    }

    private init(params: [PluginParam], texts: [String: String], flags: [String: Bool]) {
        self.params = params
        self.texts = texts
        self.flags = flags
    }

    /// The text of a string, int or enum field.
    public func text(_ name: String) -> String { texts[name] ?? "" }

    /// The switch of a bool field.
    public func flag(_ name: String) -> Bool { flags[name] ?? false }

    public func withText(_ text: String, for name: String) -> PluginActionForm {
        PluginActionForm(params: params, texts: texts.merging([name: text]) { $1 }, flags: flags)
    }

    public func withFlag(_ on: Bool, for name: String) -> PluginActionForm {
        PluginActionForm(params: params, texts: texts, flags: flags.merging([name: on]) { $1 })
    }

    /// What is wrong with a field, nil when it can be sent.
    public func problem(_ param: PluginParam) -> PluginParamProblem? {
        if case .failure(let problem) = entry(param) { return problem }
        return nil
    }

    /// Whether every field can be sent.
    public var isValid: Bool { params.allSatisfy { problem($0) == nil } }

    /// The params as KubePluginAction takes them: a JSON object of the fields given, sorted by
    /// name ("" when there is none: the defaults fill in). Nil while a field has a problem.
    public var paramsJSON: String? {
        var given: [String: PluginParamValue] = [:]
        for param in params {
            switch entry(param) {
            case .failure: return nil
            case .success(let value?): given[param.name] = value
            case .success(nil): continue
            }
        }
        if given.isEmpty { return "" }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        guard let data = try? encoder.encode(given) else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    /// A field's value to send: nil to leave it out (empty, with a default Go fills in).
    private func entry(_ param: PluginParam) -> Result<PluginParamValue?, PluginParamProblem> {
        switch param.type {
        case "bool":
            return .success(.bool(flag(param.name)))
        case "string":
            let value = text(param.name)
            if value.isEmpty && param.required { return .failure(.required) }
            if value.utf8.count > pluginParamMaxBytes { return .failure(.tooLong) }
            return .success(.string(value))
        case "int":
            let value = text(param.name).trimmingCharacters(in: .whitespaces)
            if value.isEmpty { return param.required ? .failure(.required) : .success(nil) }
            guard let number = Int64(value) else { return .failure(.notAnInteger) }
            return .success(.int(number))
        case "enum":
            let value = text(param.name)
            if value.isEmpty { return param.required ? .failure(.required) : .success(nil) }
            guard param.values.contains(value) else { return .failure(.notAChoice) }
            return .success(.string(value))
        default:
            return .failure(.unsupported)
        }
    }
}

/// A refusal of KubePluginAction, read from its message (go/ichorgo/plugin_action.go and
/// kubeAPIError): what the app says instead of Go's English.
public enum PluginActionFailure: Equatable, Sendable {
    /// enabledWhen no longer holds on the object (409 ActionDisabled): the plugin's reason.
    case disabled(reason: String)
    /// The object moved between the check and the write (409): Go's message.
    case changed(detail: String)
    /// A param Go refused, by name, with why.
    case invalidParam(name: String, detail: String)
    /// The demo cluster runs no action.
    case demo
    /// The API server refused the credentials (403): what it said.
    case forbidden(detail: String)
    /// Anything else, as Go said it.
    case other(String)

    private static let apiPrefix = "Kubernetes API ("
    private static let forbiddenPrefix = "Kubernetes API: permission denied: "
    private static let paramPrefix = "parameter \""

    public init(message: String) {
        let (status, detail) = Self.apiStatus(message)
        if status == "409 ActionDisabled" {
            self = .disabled(reason: detail)
        } else if message.contains("changed since it was checked") {
            self = .changed(detail: detail)
        } else if let param = Self.param(message) {
            self = .invalidParam(name: param.name, detail: param.detail)
        } else if message.contains("unavailable in the demo cluster") {
            self = .demo
        } else if message.hasPrefix(Self.forbiddenPrefix) {
            self = .forbidden(detail: String(message.dropFirst(Self.forbiddenPrefix.count)))
        } else {
            self = .other(message)
        }
    }

    /// "Kubernetes API (409 Conflict): text" as ("409 Conflict", "text"); ("", message) otherwise.
    private static func apiStatus(_ message: String) -> (status: String, detail: String) {
        guard message.hasPrefix(apiPrefix), let close = message.range(of: "): ") else { return ("", message) }
        let status = message[message.index(message.startIndex, offsetBy: apiPrefix.count)..<close.lowerBound]
        return (String(status), String(message[close.upperBound...]))
    }

    /// `parameter "size": want an integer, got "big"` anywhere in the message: the name and why.
    private static func param(_ message: String) -> (name: String, detail: String)? {
        guard let start = message.range(of: paramPrefix),
              let end = message.range(of: "\": ", range: start.upperBound..<message.endIndex) else { return nil }
        let name = String(message[start.upperBound..<end.lowerBound])
        guard !name.isEmpty, !name.contains("\"") else { return nil }
        return (name, String(message[end.upperBound...]))
    }

    /// Whether the tab should read again: the object it shows is not what the cluster has.
    public var staleObject: Bool {
        switch self {
        case .disabled, .changed: true
        default: false
        }
    }
}
