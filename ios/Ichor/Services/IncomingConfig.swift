import Foundation

/// A talosconfig or kubeconfig opened with Ichor ("Open with", the share sheet, AirDrop; the
/// YAML and .kubeconfig document types in project.yml): its text waits in NotificationRouter
/// for the import screen, which previews it like a picked file. Nothing is stored before the
/// user imports it there.
@MainActor
enum IncomingConfig {
    /// The largest config read, the import screen's bound too.
    static let maxBytes = 1024 * 1024

    /// Takes a file URL; other URLs (share links) are not ours.
    static func receive(_ url: URL) {
        guard url.isFileURL else { return }
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let data = try? Data(contentsOf: url)
        // The copy iOS puts in the app's Inbox holds credentials: not kept.
        if url.pathComponents.contains("Inbox") { try? FileManager.default.removeItem(at: url) }
        guard let data, data.count <= maxBytes, let text = String(data: data, encoding: .utf8) else { return }
        NotificationRouter.shared.pendingImportText = text
    }
}
