import Foundation

// Mirrors go/ichorgo/relay_pair.go: the in-cluster ichor-relay found through the Kubernetes API
// service proxy, and the device the phone pairs with it. On iOS the device is a ntfy topic: the
// ntfy app shows the relay's notification and its tap opens Ichor on the cluster's Alerts screen.

/// The relays among the cluster's Services, the likeliest first; `canPair` is false with the
/// reason (the RBAC refusal, no relay, "demo") when this config cannot read the pairing token.
public struct RelayDiscovery: Decodable, Equatable, Sendable {
    public let sources: [PromSource]
    public let canPair: Bool
    public let reason: String

    public init(sources: [PromSource] = [], canPair: Bool = false, reason: String = "") {
        self.sources = sources
        self.canPair = canPair
        self.reason = reason
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            sources: try c.field(.sources, []), canPair: try c.field(.canPair, false), reason: try c.field(.reason, ""))
    }

    private enum CodingKeys: String, CodingKey { case sources, canPair, reason }
}

/// What the phone registers; Go adds the id, the cluster and the link opened on a tap.
public struct RelayDevice: Encodable, Equatable, Sendable {
    public static let ntfy = "ntfy"

    public let platform: String
    public let endpoint: String
    /// The cluster's nickname, in the notification title; empty for none.
    public let label: String
    /// The alerts' summaries in the message too, in clear on the ntfy server.
    public let verbose: Bool

    public init(platform: String, endpoint: String, label: String, verbose: Bool) {
        self.platform = platform
        self.endpoint = endpoint
        self.label = label
        self.verbose = verbose
    }
}

/// A cluster paired with the relay from this phone, kept per cluster to show and unpair it.
/// `quiet`: the quiet hours the relay last took for this device (nil: none asked yet, the relay
/// holds none). `quietUnsupported`: the relay was too old to take them when last asked.
public struct RelayPairingRecord: Codable, Equatable, Sendable {
    public let deviceId: String
    public let endpoint: String
    public let source: PromSource
    public let pairedAt: Date
    public var quiet: QuietHours?
    public var quietUnsupported: Bool?

    public init(
        deviceId: String, endpoint: String, source: PromSource, pairedAt: Date, quiet: QuietHours? = nil,
        quietUnsupported: Bool? = nil
    ) {
        self.deviceId = deviceId
        self.endpoint = endpoint
        self.source = source
        self.pairedAt = pairedAt
        self.quiet = quiet
        self.quietUnsupported = quietUnsupported
    }

    /// The record once the relay was asked to hold `quiet`: `applied` false when it is too old to.
    public func asked(quiet: QuietHours, applied: Bool) -> RelayPairingRecord {
        var out = self
        out.quiet = quiet
        out.quietUnsupported = applied ? nil : true
        return out
    }

    /// Whether the relay is to be told `wanted` (see AlertRules.relayQuietHours): it is not what
    /// it last took. A relay too old for it is not asked again for the same window.
    public func needsQuiet(_ wanted: QuietHours) -> Bool {
        (quiet ?? .off) != wanted
    }

    /// Whether to say the relay must be updated: it could not take the quiet hours the rules set.
    public var quietNeedsRelayUpdate: Bool {
        quietUnsupported == true && !(quiet ?? .off).isOff
    }
}

extension AlertRules {
    /// The quiet hours the relay holds for the cluster `fingerprint`'s device: its override,
    /// else the default's, with the phone's `zone` when it names none; `.off` for no window.
    /// The ntfy app shows the relay's pushes, so the phone cannot hold them back itself.
    public func relayQuietHours(_ fingerprint: String, zone: String) -> QuietHours {
        guard var quiet = clusters[fingerprint]?.quietHours ?? self.default.quietHours, !quiet.isOff
        else { return .off }
        if quiet.tz.trimmingCharacters(in: .whitespaces).isEmpty { quiet.tz = zone }
        return quiet
    }
}

/// The ntfy server offered first: the public one, with a topic nobody can guess.
public let ntfyDefaultServer = "https://ntfy.sh"

private let ntfyTopicChars = Array("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

/// A random 16-character topic: on a public server, whoever knows it reads the notifications.
public func randomNtfyTopic() -> String {
    String((0..<16).map { _ in ntfyTopicChars.randomElement()! })
}

/// Whether `topic` is a ntfy topic name: 1 to 64 of letters, digits, `-` and `_`.
public func isNtfyTopic(_ topic: String) -> Bool {
    (1...64).contains(topic.count) && topic.unicodeScalars.allSatisfy(isNtfyTopicScalar)
}

private func isNtfyTopicScalar(_ scalar: Unicode.Scalar) -> Bool {
    switch scalar {
    case "a"..."z", "A"..."Z", "0"..."9", "-", "_": true
    default: false
    }
}

/// The topic's https URL on `server` (an https origin, a trailing slash allowed, nothing else
/// after the host); nil when either is not valid.
public func ntfyTopicURL(server: String, topic: String) -> URL? {
    let topic = topic.trimmingCharacters(in: .whitespaces)
    guard isNtfyTopic(topic),
        var parts = URLComponents(string: server.trimmingCharacters(in: .whitespaces)),
        parts.scheme?.lowercased() == "https", let host = parts.host, !host.isEmpty,
        parts.user == nil, parts.password == nil, parts.query == nil, parts.fragment == nil,
        parts.path.isEmpty || parts.path == "/"
    else { return nil }
    parts.scheme = "https"
    parts.path = "/" + topic
    return parts.url
}

/// The ntfy app's link subscribing to the topic at `topicURL` (ntfy://host[:port]/topic).
public func ntfyAppURL(_ topicURL: URL) -> URL? {
    guard var parts = URLComponents(url: topicURL, resolvingAgainstBaseURL: false), parts.host != nil else {
        return nil
    }
    parts.scheme = "ntfy"
    return parts.url
}

/// "ntfy.sh/topic": the topic URL without its scheme, for the paired row.
public func ntfyTopicLabel(_ endpoint: String) -> String {
    guard let parts = URLComponents(string: endpoint), let host = parts.host else { return endpoint }
    let port = parts.port.map { ":\($0)" } ?? ""
    return host + port + parts.path
}
