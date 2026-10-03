import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// Read-only machine config (os:admin), secrets masked unless the user reveals them.
struct MachineConfigView: View {
    let node: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<String> = .loading
    @State private var reveal = false
    @State private var query = ""
    @State private var message: String?

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                Toggle(isOn: Binding(get: { reveal }, set: { on in Task { await setReveal(on) } })) {
                    Label("Reveal secrets", systemImage: reveal ? "eye" : "eye.slash")
                }
                if reveal {
                    Label("Secrets are shown in clear: CA keys and tokens give full control of the cluster. Don't share screenshots.",
                          systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
                if let message {
                    Text(message).font(.footnote).foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 8)

            LoadStateView(state: state, retry: load) { yaml in
                let lines = filterLines(yaml, query: query)
                List(lines) { line in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(verbatim: "\(line.number)")
                            .font(.caption2.monospacedDigit())
                            .foregroundStyle(.tertiary)
                            .frame(minWidth: 28, alignment: .trailing)
                        Text(verbatim: line.text)
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .listRowInsets(EdgeInsets(top: 1, leading: 8, bottom: 1, trailing: 8))
                    .listRowSeparator(.hidden)
                }
                .listStyle(.plain)
                .environment(\.defaultMinListRowHeight, 0)
                .overlay {
                    if lines.isEmpty && !query.isEmpty { ContentUnavailableView.search(text: query) }
                }
                .refreshable { await load() }
                .themedBackground()
            }
        }
        .navigationTitle("Machine config")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $query, prompt: Text("Filter lines"))
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(action: copy) {
                    Label("Copy", systemImage: "doc.on.doc")
                }
                .disabled(loadedYAML == nil)
            }
        }
        .task { await load() }
    }

    private var loadedYAML: String? {
        if case .loaded(let yaml, _, _) = state { return yaml }
        return nil
    }

    /// Revealing needs a fresh Face ID / passcode when the app lock is on; hiding re-fetches
    /// the redacted config so the secrets leave memory and screen.
    private func setReveal(_ on: Bool) async {
        if on, model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Reveal the secrets of \(hostname)")) {
            message = failure
            return
        }
        reveal = on
        message = nil
        state = .loading
        await load()
    }

    private func load() async {
        guard let client = model.client else { return }
        let wanted = reveal
        let result: LoadState<String> = await .from { try await client.machineConfig(node: node, revealSecrets: wanted) }
        if wanted == reveal { state = result } // a newer toggle wins
    }

    /// Device-only pasteboard; revealed secrets expire from it after two minutes.
    private func copy() {
        guard let yaml = loadedYAML else { return }
        let options: [UIPasteboard.OptionsKey: Any] = reveal
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: yaml]], options: options)
        message = reveal ? String(localized: "Copied. The clipboard is cleared in 2 minutes.") : String(localized: "Copied.")
    }
}
