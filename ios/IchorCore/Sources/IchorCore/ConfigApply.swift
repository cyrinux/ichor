import Foundation

// Mirrors go/ichorgo/configapply_modes.go (StartConfigApply).

/// How a reviewed change is applied for good (try mode aside); the raw value is StartConfigApply's mode.
public enum ConfigApplyMode: String, CaseIterable, Identifiable, Sendable {
    /// Now, without a reboot: only for a change that needs none.
    case auto
    /// At the next reboot, which is not asked for.
    case staged
    /// Now, then the node reboots: confirmed with the typed hostname.
    case reboot

    public var id: String { rawValue }
}

extension ConfigPreview {
    /// The modes this change allows: one that needs a reboot cannot be applied now without one.
    public var applyModes: [ConfigApplyMode] { needsReboot ? [.staged, .reboot] : ConfigApplyMode.allCases }
}

/// A phase change of a config applied for good (ConfigApplyListener.OnProgress).
public struct ConfigApplyProgress: Decodable, Equatable, Sendable {
    public enum Phase: String, Sendable {
        case applying, rebooting, waiting, done
    }

    /// Go's phase; an unknown one reads as applying.
    public let phase: String
    public let message: String
    /// Unix milliseconds.
    public let at: Int64

    public var applyPhase: Phase { Phase(rawValue: phase) ?? .applying }

    public init(phase: String, message: String = "", at: Int64 = 0) {
        self.phase = phase
        self.message = message
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
    }
}
