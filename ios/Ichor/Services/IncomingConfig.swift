import Foundation
import IchorCore

/// A talosconfig, kubeconfig or backup opened with Ichor ("Open with", AirDrop; the YAML and
/// .kubeconfig document types in project.yml) or sent to it from the share sheet (the IchorShare
/// extension leaves it in the App Group, taken on the next activation). A config's text waits
/// in NotificationRouter for the import screen, which previews it like a picked file; a
/// backup's bytes for the restore flow. Nothing is stored before the user imports or restores.
@MainActor
enum IncomingConfig {
    /// The largest config read, the import screen's bound too.
    static let maxBytes = 1024 * 1024

    /// Takes a file URL; other URLs (share links) are not ours.
    static func receive(_ url: URL) {
        guard url.isFileURL else { return }
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let kind = IncomingFile.classify(name: url.lastPathComponent, head: IncomingFile.head(of: url))
        let data = try? Data(contentsOf: url)
        // The copy iOS puts in the app's Inbox holds credentials: not kept.
        if url.pathComponents.contains("Inbox") { try? FileManager.default.removeItem(at: url) }
        guard let data else { return }
        switch kind {
        case .backup:
            guard data.count <= IncomingFile.maxBytes else { return }
            NotificationRouter.shared.pendingBackup = data
        case .config:
            guard data.count <= maxBytes, let text = String(data: data, encoding: .utf8) else { return }
            NotificationRouter.shared.pendingImportText = text
        case .unsupported:
            return
        }
    }

    /// The files "Send to Ichor" left in the App Group: each through `receive`, oldest first (the
    /// newest is the one on screen), then deleted; one older than a day is deleted unread.
    static func receivePending() {
        guard let group = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: SharedSnapshot.suite) else { return }
        let inbox = group.appendingPathComponent(IncomingFile.inboxFolder, isDirectory: true)
        let pending = IncomingFile.pending(in: inbox)
        for url in pending.stale { try? FileManager.default.removeItem(at: url) }
        for url in pending.take {
            receive(url)
            try? FileManager.default.removeItem(at: url)
        }
    }
}
