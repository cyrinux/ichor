import Foundation

/// The "Enter details" form of the add-cluster screen: a Talos cluster reached directly
/// (endpoints, CA, client certificate) or through Omni (instance, cluster name, and who signs
/// in). Go's BuildTalosconfig turns `json()` into a talosconfig. Same rules as Android.
public struct TalosForm: Equatable, Sendable {
    public enum Mode: String, CaseIterable, Sendable {
        case direct, omni
    }

    public var mode: Mode
    public var name: String
    /// One address per line.
    public var endpoints: String
    public var nodes: String
    /// PEM as the files hold it, or base64 PEM as a talosconfig does.
    public var ca: String
    public var crt: String
    public var key: String
    public var omniURL: String
    public var cluster: String
    /// The Omni account's email, or a service account's identity.
    public var identity: String

    public init(mode: Mode = .direct, name: String = "", endpoints: String = "", nodes: String = "",
                ca: String = "", crt: String = "", key: String = "", omniURL: String = "", cluster: String = "",
                identity: String = "") {
        self.mode = mode
        self.name = name
        self.endpoints = endpoints
        self.nodes = nodes
        self.ca = ca
        self.crt = crt
        self.key = key
        self.omniURL = omniURL
        self.cluster = cluster
        self.identity = identity
    }

    /// The trimmed, non-empty lines of `text`.
    public static func lines(_ text: String) -> [String] {
        text.split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    /// Every field the mode needs is filled (Go checks what they hold).
    public var canSubmit: Bool {
        guard !Self.blank(name) else { return false }
        switch mode {
        case .direct:
            return !Self.lines(endpoints).isEmpty && !Self.blank(ca) && !Self.blank(crt) && !Self.blank(key)
        case .omni:
            return !Self.blank(omniURL) && !Self.blank(cluster) && !Self.blank(identity)
        }
    }

    /// The form as BuildTalosconfig takes it: the mode's fields only, lists split by line.
    public func json() throws -> String {
        let payload: Payload
        switch mode {
        case .direct:
            let nodeList = Self.lines(nodes)
            payload = Payload(mode: mode.rawValue, name: Self.trimmed(name), endpoints: Self.lines(endpoints),
                              nodes: nodeList.isEmpty ? nil : nodeList,
                              ca: Self.trimmed(ca), crt: Self.trimmed(crt), key: Self.trimmed(key))
        case .omni:
            payload = Payload(mode: mode.rawValue, name: Self.trimmed(name), omniUrl: Self.trimmed(omniURL),
                              cluster: Self.trimmed(cluster), identity: Self.trimmed(identity))
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return String(decoding: try encoder.encode(payload), as: UTF8.self)
    }

    private struct Payload: Encodable {
        let mode: String
        let name: String
        var endpoints: [String]?
        var nodes: [String]?
        var ca: String?
        var crt: String?
        var key: String?
        var omniUrl: String?
        var cluster: String?
        var identity: String?
    }

    private static func trimmed(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func blank(_ text: String) -> Bool {
        trimmed(text).isEmpty
    }
}
