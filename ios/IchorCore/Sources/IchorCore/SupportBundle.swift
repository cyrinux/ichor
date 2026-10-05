import Foundation

/// Progress of one node of a support bundle (SupportListener.OnProgress): the step that
/// starts and how many of the node's steps are finished. The cluster-wide part reports under
/// node "".
public struct SupportProgress: Decodable, Equatable, Sendable {
    public let node: String
    /// What is being collected, in Go's words ("kernel log", "service logs"…).
    public let step: String
    public let done: Int
    public let total: Int

    public init(node: String, step: String = "", done: Int = 0, total: Int = 0) {
        self.node = node
        self.step = step
        self.done = done
        self.total = total
    }

    private enum CodingKeys: String, CodingKey { case node, step, done, total }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        step = try c.field(.step, "")
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
    }

    /// In [0, 1]; nil while the total is unknown.
    public var fraction: Double? {
        guard total > 0 else { return nil }
        return min(max(Double(done) / Double(total), 0), 1)
    }

    /// A node is finished when all its steps are done.
    public var finished: Bool { total > 0 && done >= total }
}

/// Where one node (or the cluster-wide part, node "") stands in a bundle.
public enum SupportNodeState: Equatable, Sendable {
    case waiting
    case collecting(SupportProgress)
    case done
}

/// The latest progress of each node of a bundle, the cluster-wide part under "".
///
/// Go counts `done`/`total` per node (a node is finished when done == total). An older core
/// counts the steps of the whole bundle instead, one section after the other: that shows when
/// a section's first report already has steps done, and then the section reporting is the one
/// being collected, the earlier ones are done, and the closing report finishes them all.
public struct SupportBundleProgress: Equatable, Sendable {
    public let latest: [String: SupportProgress]
    /// The section of the latest report.
    public let current: String?
    public let countsWholeBundle: Bool
    /// Whole-bundle counting only: the closing report came.
    public let closed: Bool

    public init(latest: [String: SupportProgress] = [:], current: String? = nil, countsWholeBundle: Bool = false, closed: Bool = false) {
        self.latest = latest
        self.current = current
        self.countsWholeBundle = countsWholeBundle
        self.closed = closed
    }

    public func recording(_ progress: SupportProgress) -> SupportBundleProgress {
        var next = latest
        next[progress.node] = progress
        let whole = countsWholeBundle || (latest[progress.node] == nil && progress.done > 0 && !progress.finished)
        return SupportBundleProgress(latest: next, current: progress.node, countsWholeBundle: whole,
                                     closed: closed || (whole && progress.finished))
    }

    /// Share of the rows that are done, the selected nodes plus the cluster-wide part.
    public func fractionDone(nodes: [String]) -> Double {
        let rows = nodes + [""]
        return Double(rows.filter { state(of: $0) == .done }.count) / Double(rows.count)
    }

    public func state(of node: String) -> SupportNodeState {
        if closed { return .done }
        guard let progress = latest[node] else { return .waiting }
        if countsWholeBundle { return node == current ? .collecting(progress) : .done }
        return progress.finished ? .done : .collecting(progress)
    }
}

/// A file a cancelled or killed collection left behind (Go writes NAME.part, then renames).
public func isStaleSupportPartName(_ name: String) -> Bool {
    name.hasSuffix(".part") && isSupportBundleName(String(name.dropLast(".part".count)))
}

/// The nodes to collect from, as Go wants them: the selected ones in the cluster's order.
public func supportNodesCSV(all: [String], selected: Set<String>) -> String {
    all.filter(selected.contains).joined(separator: ",")
}

/// "support-<context>-<yyyyMMdd-HHmmss>.zip", safe as a file name whatever the context is
/// called: runs of other characters become one "-", at most 40 characters of context.
public func supportBundleFilename(context: String, date: Date, timeZone: TimeZone = .current) -> String {
    var safe = ""
    for character in context {
        if isBundleNameCharacter(character) {
            safe.append(character)
        } else if !safe.hasSuffix("-") {
            safe.append("-")
        }
    }
    let name = String(safe.trimmingCharacters(in: CharacterSet(charactersIn: "-.")).prefix(40))
    return "support-\(name.isEmpty ? "cluster" : name)-\(fileTimestamp(date, format: "yyyyMMdd-HHmmss", timeZone: timeZone)).zip"
}

private func isBundleNameCharacter(_ character: Character) -> Bool {
    character.isASCII && (character.isLetter || character.isNumber || character == "." || character == "_" || character == "-")
}

/// Only files this app wrote are listed, shared or deleted.
public func isSupportBundleName(_ name: String) -> Bool {
    guard name.hasPrefix("support-"), name.hasSuffix(".zip") else { return false }
    let middle = name.dropFirst("support-".count).dropLast(".zip".count)
    return !middle.isEmpty && middle.allSatisfy(isBundleNameCharacter)
}

/// A bundle kept on the phone.
public struct SupportBundleFile: Equatable, Identifiable, Sendable {
    public let name: String
    public let size: Int64
    public let modified: Date

    public var id: String { name }

    public init(name: String, size: Int64, modified: Date) {
        self.name = name
        self.size = size
        self.modified = modified
    }
}

/// Newest first, then by name.
public func sortSupportBundles(_ files: [SupportBundleFile]) -> [SupportBundleFile] {
    files.sorted { a, b in
        if a.modified != b.modified { return a.modified > b.modified }
        return a.name < b.name
    }
}
