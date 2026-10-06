import Foundation

/// One sample of cumulative counters from the Go core (NodeStats).
public struct NodeStats: Codable, Equatable, Sendable {
    public let at: Int64
    public let cpuBusy: Double
    public let cpuTotal: Double
    public let cpuCount: Int
    public let memTotal: UInt64
    public let memAvailable: UInt64
    public let load1: Double
    public let netRx: UInt64
    public let netTx: UInt64
    public let diskRead: UInt64
    public let diskWrite: UInt64
    public var cpuWait: Double? = nil
    public var cpuSteal: Double? = nil
    public var bootTime: UInt64? = nil
    public var networkDevices: [NetworkCounters]? = nil
    public var diskDevices: [DiskCounters]? = nil
    public var errors: [String: String]? = nil
}

/// Rates between two samples, ready to plot (same rules as Android).
public struct StatsPoint: Equatable, Identifiable, Sendable {
    public let at: Date
    public let cpuPercent: Double
    public let memPercent: Double
    public let memUsed: UInt64
    public let load1: Double
    public let rxPerSec: Double
    public let txPerSec: Double
    public let readPerSec: Double
    public let writePerSec: Double

    public var id: Date { at }
}

/// Counters that went backwards (node reboot, interface reset) give 0, not a negative spike;
/// nil when no time elapsed.
public func ratesBetween(_ prev: NodeStats, _ cur: NodeStats) -> StatsPoint? {
    guard (prev.errors ?? [:]).isEmpty, (cur.errors ?? [:]).isEmpty else { return nil }
    if let a = prev.bootTime, let b = cur.bootTime, a != 0, b != 0, a != b { return nil }
    let seconds = Double(cur.at - prev.at) / 1000
    guard seconds > 0 else { return nil }

    func perSec(_ a: UInt64, _ b: UInt64) -> Double { b >= a ? Double(b - a) / seconds : 0 }

    let cpuDelta = cur.cpuTotal - prev.cpuTotal
    let cpu = cpuDelta > 0 ? min(max((cur.cpuBusy - prev.cpuBusy) / cpuDelta * 100, 0), 100) : 0
    let used = cur.memAvailable >= cur.memTotal ? 0 : cur.memTotal - cur.memAvailable
    let mem = cur.memTotal > 0 ? Double(used) * 100 / Double(cur.memTotal) : 0

    return StatsPoint(
        at: Date(epochMillis: cur.at),
        cpuPercent: cpu,
        memPercent: mem,
        memUsed: used,
        load1: cur.load1,
        rxPerSec: perSec(prev.netRx, cur.netRx),
        txPerSec: perSec(prev.netTx, cur.netTx),
        readPerSec: perSec(prev.diskRead, cur.diskRead),
        writePerSec: perSec(prev.diskWrite, cur.diskWrite)
    )
}

/// Occasional "support the project" prompt: after real use (2 weeks, 10 launches), at most
/// every 90 days, never again once declined for good.
public struct SupportState: Equatable, Sendable {
    public var firstSeen: Date
    public var launches: Int
    public var lastAsked: Date?
    public var never: Bool

    public init(firstSeen: Date, launches: Int, lastAsked: Date? = nil, never: Bool = false) {
        self.firstSeen = firstSeen
        self.launches = launches
        self.lastAsked = lastAsked
        self.never = never
    }

    public func shouldAsk(now: Date) -> Bool {
        let day: TimeInterval = 86_400
        guard !never, launches >= 10, now.timeIntervalSince(firstSeen) >= 14 * day else { return false }
        guard let lastAsked else { return true }
        return now.timeIntervalSince(lastAsked) >= 90 * day
    }
}
