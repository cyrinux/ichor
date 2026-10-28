import Foundation

/// The line under a screen's title that names what it acts on: `subtitle` (a node's hostname)
/// and, with several clusters on the phone, the active cluster's label after it ("node-a ·
/// lab"). Nil when there is nothing to say: one cluster and no subtitle. Same rule as
/// Android's `clusterTitleLine`.
public func clusterTitleLine(clusterCount: Int, activeLabel: String?, subtitle: String? = nil) -> String? {
    let cluster = clusterCount > 1 ? activeLabel.flatMap { $0.isEmpty ? nil : $0 } : nil
    let sub = subtitle.flatMap { $0.trimmingCharacters(in: .whitespaces).isEmpty ? nil : $0 }
    let parts = [sub, cluster].compactMap { $0 }
    return parts.isEmpty ? nil : parts.joined(separator: " · ")
}
