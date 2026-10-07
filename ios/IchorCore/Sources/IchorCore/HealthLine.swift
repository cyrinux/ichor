import Foundation

/// How one progress line of the cluster health check reads, for its colour.
public enum HealthLineStatus: Equatable, Sendable {
    case ok, pending, warn, bad, info
}

/// Classifies a Talos health line ("waiting for <condition>: <state>"): the state is "OK" once
/// the condition holds, "..." before its first evaluation, and otherwise why it does not hold yet.
/// `failed` marks the line the check gave up on, the last one of a run that ended in an error.
public func healthLineStatus(_ line: String, failed: Bool = false) -> HealthLineStatus {
    let text = line.trimmingCharacters(in: .whitespacesAndNewlines)
    if text.hasSuffix(": OK") { return .ok }
    guard text.hasPrefix("waiting for ") else { return .info }
    if failed { return .bad }
    return text.hasSuffix("...") || !text.contains(": ") ? .pending : .warn
}
