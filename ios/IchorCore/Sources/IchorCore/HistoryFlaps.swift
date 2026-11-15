import Foundation

/// HistoryAlertFlaps (go/ichorgo/history_flaps.go): the alerts of a cluster's history ring that
/// opened more than once in `windowHours`, most opens first.
public struct HistoryFlaps: Decodable, Equatable, Sendable {
    public let windowHours: Int
    public let alerts: [HistoryFlap]

    public init(windowHours: Int = 24, alerts: [HistoryFlap] = []) {
        self.windowHours = windowHours
        self.alerts = alerts
    }

    /// The opens of each alert key (the notification's, see `historyAlertKey`), for the alert
    /// rules' flapping check and the History marker.
    public var opensByAlertKey: [String: Int] {
        Dictionary(alerts.map { (historyAlertKey(track: $0.track, key: $0.key), $0.opens) }, uniquingKeysWith: max)
    }

    private enum CodingKeys: String, CodingKey { case windowHours, alerts }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        windowHours = try c.field(.windowHours, 24)
        alerts = try c.field(.alerts, [])
    }
}

/// One ring key: how many times it opened and closed in the window, whether it is open now.
public struct HistoryFlap: Decodable, Equatable, Sendable {
    public let key: String
    public let track: String
    public let opens: Int
    public let closes: Int
    public let open: Bool
    public let lastOpenedAt: Int64

    public init(
        key: String, track: String = "", opens: Int, closes: Int = 0, open: Bool = false, lastOpenedAt: Int64 = 0
    ) {
        self.key = key
        self.track = track
        self.opens = opens
        self.closes = closes
        self.open = open
        self.lastOpenedAt = lastOpenedAt
    }

    private enum CodingKeys: String, CodingKey { case key, track, opens, closes, open, lastOpenedAt }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        track = try c.field(.track, "")
        opens = try c.field(.opens, 0)
        closes = try c.field(.closes, 0)
        open = try c.field(.open, false)
        lastOpenedAt = try c.field(.lastOpenedAt, 0)
    }
}
