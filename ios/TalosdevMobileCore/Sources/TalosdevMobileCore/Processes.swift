import Foundation

/// One `talosctl processes` sample from the Go core (NodeProcesses).
public struct ProcessSample: Decodable, Equatable, Sendable {
    /// Unix milliseconds, to turn CPU time deltas into a percentage.
    public let at: Int64
    public let processes: [NodeProcess]

    public init(at: Int64, processes: [NodeProcess]) {
        self.at = at
        self.processes = processes
    }

    private enum CodingKeys: String, CodingKey { case at, processes }

    // Go encodes an empty (nil) slice as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.decode(Int64.self, forKey: .at)
        processes = try c.decodeIfPresent([NodeProcess].self, forKey: .processes) ?? []
    }
}

public struct NodeProcess: Decodable, Equatable, Identifiable, Sendable {
    public let pid: Int32
    public let ppid: Int32
    public let state: String
    public let threads: Int32
    /// Cumulative CPU seconds.
    public let cpuTime: Double
    /// Resident memory, bytes.
    public let rss: UInt64
    /// Virtual memory, bytes.
    public let vms: UInt64
    public let command: String
    public let args: String

    public var id: Int32 { pid }

    public init(pid: Int32, ppid: Int32 = 0, state: String = "S", threads: Int32 = 1, cpuTime: Double = 0,
                rss: UInt64 = 0, vms: UInt64 = 0, command: String, args: String = "") {
        self.pid = pid
        self.ppid = ppid
        self.state = state
        self.threads = threads
        self.cpuTime = cpuTime
        self.rss = rss
        self.vms = vms
        self.command = command
        self.args = args
    }
}

/// A process with its CPU usage since the previous sample (nil until there is one).
public struct ProcessRow: Equatable, Identifiable, Sendable {
    public let process: NodeProcess
    /// Percent of one CPU (can exceed 100 for multi-threaded processes, like top).
    public let cpuPercent: Double?

    public var id: Int32 { process.pid }

    public init(process: NodeProcess, cpuPercent: Double?) {
        self.process = process
        self.cpuPercent = cpuPercent
    }
}

public enum ProcessSort: String, CaseIterable, Sendable {
    case cpu, memory
}

/// CPU% per pid from the CPU time delta over the wall-clock delta (×100). A pid whose command
/// changed (reused pid) or that is new has no value yet; a counter that went backwards gives 0.
public func processRows(previous: ProcessSample?, current: ProcessSample) -> [ProcessRow] {
    var before: [Int32: NodeProcess] = [:]
    var seconds = 0.0
    if let previous {
        seconds = Double(current.at - previous.at) / 1000
        for process in previous.processes { before[process.pid] = process }
    }
    return current.processes.map { process in
        guard seconds > 0, let old = before[process.pid], old.command == process.command else {
            return ProcessRow(process: process, cpuPercent: nil)
        }
        let percent = max(process.cpuTime - old.cpuTime, 0) / seconds * 100
        return ProcessRow(process: process, cpuPercent: percent)
    }
}

/// Highest CPU (unknown last) or highest RSS first; ties by RSS then pid, so rows don't jump.
public func sortProcesses(_ rows: [ProcessRow], by sort: ProcessSort) -> [ProcessRow] {
    rows.sorted { a, b in
        if sort == .cpu {
            let ca = a.cpuPercent ?? -1
            let cb = b.cpuPercent ?? -1
            if ca != cb { return ca > cb }
        }
        if a.process.rss != b.process.rss { return a.process.rss > b.process.rss }
        return a.process.pid < b.process.pid
    }
}

/// Case-insensitive match on command or arguments; an empty query keeps everything.
public func filterProcesses(_ rows: [ProcessRow], query: String) -> [ProcessRow] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return rows }
    return rows.filter {
        $0.process.command.range(of: needle, options: .caseInsensitive) != nil
            || $0.process.args.range(of: needle, options: .caseInsensitive) != nil
    }
}

public func totalRSS(_ processes: [NodeProcess]) -> UInt64 {
    processes.reduce(UInt64(0)) { $0 &+ $1.rss }
}
