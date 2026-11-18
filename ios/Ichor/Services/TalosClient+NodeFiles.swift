import Foundation
import IchorCore
import Ichorgo

/// A read-only file browser on a node, like talosctl ls and talosctl read (os:reader).
extension TalosClient {
    /// The entries directly below `dir` on the node ("" for /var/log); not cached.
    func nodeFiles(node: String, dir: String) async throws -> NodeDirListing {
        try await Self.json { [config, context] in IchorgoNodeFiles(config, context, node, dir, $0) }
    }

    /// The text of `path` on the node, capped at 512 KiB, secrets under /system/state redacted.
    func nodeFileRead(node: String, path: String) async throws -> NodeFileText {
        try await Self.json { [config, context] in IchorgoNodeFileRead(config, context, node, path, $0) }
    }
}
