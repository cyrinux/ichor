import Foundation
import IchorCore
import Ichorgo

/// The action audit log the core keeps on this device (go/ichorgo/audit.go): every change the
/// app made to a cluster. No cluster call, so no client needed (static: also for a VPN-only
/// cluster held back). `cluster`: a context name, or "" for every cluster.
extension TalosClient {
    /// The entries of `cluster`, newest first.
    static func activity(cluster: String) async throws -> [ActivityEntry] {
        try await json { IchorgoAuditLog(cluster, "", $0) }
    }

    /// The log of `cluster` as a Markdown table, to share.
    static func activityExport(cluster: String) async throws -> String {
        try await run { IchorgoAuditExport(cluster, "markdown", $0) }
    }

    /// Forgets the entries of `cluster`.
    static func clearActivity(cluster: String) async throws {
        try await run { error -> Void in _ = IchorgoAuditClear(cluster, error) }
    }
}
