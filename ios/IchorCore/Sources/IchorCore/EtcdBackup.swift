import Foundation

/// Member to take a snapshot from by default: a healthy follower (keeps load off the
/// leader), else any healthy voting member, else nil.
public func defaultSnapshotMember(_ statuses: [EtcdNodeStatus]) -> EtcdNodeStatus? {
    let healthy = statuses.filter { $0.error == nil && $0.errors.isEmpty && !$0.memberId.isEmpty }
    return healthy.first { !$0.isLeader && !$0.isLearner } ?? healthy.first { !$0.isLearner }
}

/// "etcd-<context>-<hostname>-<yyyyMMdd-HHmm>.snapshot", with characters that are awkward in
/// file names replaced by "-".
public func etcdSnapshotFilename(context: String, hostname: String, date: Date,
                                 timeZone: TimeZone = .current) -> String {
    return "etcd-\(fileSafe(context))-\(fileSafe(hostname))-\(fileTimestamp(date, format: "yyyyMMdd-HHmm", timeZone: timeZone)).snapshot"
}

private func fileSafe(_ text: String) -> String {
    let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "._-"))
    let mapped = text.unicodeScalars.map { allowed.contains($0) ? String($0) : "-" }.joined()
    return mapped.isEmpty ? "unknown" : mapped
}

/// Progress in [0, 1] when the expected size is known, nil otherwise (indeterminate).
public func snapshotFraction(bytes: Int64, expected: Int64) -> Double? {
    guard expected > 0 else { return nil }
    return min(max(Double(bytes) / Double(expected), 0), 1)
}

/// A line of text with its 1-based number, for the machine config viewer.
public struct NumberedLine: Equatable, Identifiable, Sendable {
    public let number: Int
    public let text: String

    public var id: Int { number }

    public init(number: Int, text: String) {
        self.number = number
        self.text = text
    }
}

/// Lines containing the query (case-insensitive), keeping their original numbers.
public func filterLines(_ text: String, query: String) -> [NumberedLine] {
    var parts = text.split(separator: "\n", omittingEmptySubsequences: false)
    if parts.count > 1, parts.last?.isEmpty == true { parts.removeLast() } // trailing newline
    let lines = parts.enumerated().map { NumberedLine(number: $0.offset + 1, text: String($0.element)) }
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return lines }
    return lines.filter { $0.text.range(of: needle, options: .caseInsensitive) != nil }
}
