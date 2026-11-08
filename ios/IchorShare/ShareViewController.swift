import IchorCore
import Observation
import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// "Send to Ichor" in the share sheet: copies the shared config or backup into the App Group's
/// inbox (IncomingFile), where the app takes it on its next activation, and says so. A share
/// extension cannot open its app, so the user opens Ichor themselves.
final class ShareViewController: UIViewController {
    private let state = ShareState()

    override func viewDidLoad() {
        super.viewDidLoad()
        let host = UIHostingController(rootView: ShareView(state: state) { [weak self] in self?.finish() })
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)

        let providers = (extensionContext?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        Task { @MainActor in
            state.outcome = await ShareInbox.save(providers.first)
        }
    }

    private func finish() {
        extensionContext?.completeRequest(returningItems: nil)
    }
}

/// How sending the file ended.
enum ShareOutcome: Equatable {
    case saved, tooLarge, unsupported, notProtected, failed
}

@MainActor
@Observable
final class ShareState {
    /// nil while the file is being copied.
    var outcome: ShareOutcome?
}

/// Copies a shared file into the App Group's inbox, streamed (an extension has little memory).
enum ShareInbox {
    /// The types asked of the provider, most specific first; a file without one is plain data.
    private static let types = [
        "name.levis.ichor.kubeconfig", "name.levis.ichor.backup", "name.levis.ichor.agebackup",
        UTType.yaml.identifier, UTType.data.identifier,
    ]

    @MainActor
    static func save(_ provider: NSItemProvider?) async -> ShareOutcome {
        guard let provider, let type = types.first(where: { provider.hasItemConformingToTypeIdentifier($0) }) else {
            return .unsupported
        }
        guard let group = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: SharedSnapshot.suite)
        else { return .failed }
        let inbox = group.appendingPathComponent(IncomingFile.inboxFolder, isDirectory: true)
        return await withCheckedContinuation { continuation in
            // The file only exists until the handler returns: copied there.
            _ = provider.loadFileRepresentation(forTypeIdentifier: type) { @Sendable url, _ in
                continuation.resume(returning: copy(url, to: inbox))
            }
        }
    }

    private static func copy(_ url: URL?, to inbox: URL) -> ShareOutcome {
        guard let url else { return .failed }
        let size = (try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0
        guard size <= IncomingFile.maxBytes else { return .tooLarge }
        let name = url.lastPathComponent
        guard IncomingFile.classify(name: name, head: IncomingFile.head(of: url)) != .unsupported else {
            return .unsupported
        }
        do {
            try IncomingFile.stage(url, in: inbox)
            return .saved
        } catch IncomingFileError.notProtected {
            return .notProtected
        } catch {
            return .failed
        }
    }
}

/// One line saying what happened, and Done.
struct ShareView: View {
    let state: ShareState
    let done: () -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                switch state.outcome {
                case nil:
                    ProgressView()
                case .saved:
                    Label("Saved. Open Ichor to import it.", systemImage: "checkmark.circle.fill")
                case .tooLarge:
                    Label("This file is too large for a config or a backup.", systemImage: "exclamationmark.triangle")
                case .unsupported:
                    Label(
                        "Ichor takes Talos and Kubernetes configs and Ichor backups.",
                        systemImage: "exclamationmark.triangle")
                case .notProtected:
                    Label("The file could not be protected, so it was not saved.", systemImage: "lock.slash")
                case .failed:
                    Label("The file could not be saved.", systemImage: "exclamationmark.triangle")
                }
            }
            .font(.body)
            .multilineTextAlignment(.center)
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .navigationTitle("Send to Ichor")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", action: done).disabled(state.outcome == nil)
                }
            }
        }
    }
}
