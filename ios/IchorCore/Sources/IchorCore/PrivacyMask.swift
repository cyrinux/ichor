import Foundation

/// The screenshot mode's extra words as Go gets them: trimmed, without empty entries or
/// duplicates (first spelling kept), joined with "," (same as Android).
public func normalizedMaskWords(_ text: String) -> String {
    var seen = Set<String>()
    return text.split(separator: ",")
        .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        .filter { !$0.isEmpty && seen.insert($0).inserted }
        .joined(separator: ",")
}
