import SwiftUI
import IchorCore

/// What applying the draft changes, validated by the node with a dry run, then the try: the
/// node applies it and reverts by itself unless the change is kept (os:admin).
struct ConfigReviewView: View {
    let node: String
    let hostname: String
    let base: String
    let draft: String
    /// A try was started and is over, whatever its result.
    let onFinished: () -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var preview: LoadState<ConfigPreview> = .loading
    @State private var timeout = 300
    @State private var confirming = false
    @State private var trying = false
    @State private var message: String?

    var body: some View {
        NavigationStack {
            LoadStateView(state: preview, retry: load) { preview in content(preview) }
                .navigationTitle("Review changes")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                }
        }
        .task { await load() }
        .confirmationDialog(Text("Try this change on \(hostname)?"), isPresented: $confirming, titleVisibility: .visible) {
            Button("Try") { Task { await startTry() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The node applies it now and reverts by itself after \(timeout / 60) min unless you keep it.")
        }
        .fullScreenCover(isPresented: $trying, onDismiss: onFinished) {
            ConfigTryView(node: node, hostname: hostname, base: base, draft: draft, timeoutSeconds: timeout)
        }
    }

    private func content(_ preview: ConfigPreview) -> some View {
        List {
            if preview.changed {
                Section("Changes") {
                    ConfigDiffView(lines: preview.lines)
                        .listRowInsets(EdgeInsets())
                }
                if preview.needsReboot {
                    Section {
                        Label("This change needs a reboot, so it cannot be tried with automatic revert.",
                              systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.statusWarn)
                    }
                } else {
                    trySection
                }
            } else {
                Section { Text("No changes").note() }
            }
        }
        .themedBackground()
    }

    private var trySection: some View {
        Section {
            Picker("Revert after", selection: $timeout) {
                ForEach(configTryTimeouts, id: \.self) { seconds in
                    Text("\(seconds / 60) min").tag(seconds)
                }
            }
            .pickerStyle(.segmented)
            Button { confirming = true } label: {
                Text("Try").fontWeight(.semibold).frame(maxWidth: .infinity)
            }
            if let message { Text(verbatim: message).font(.footnote).foregroundStyle(.statusBad) }
        } header: {
            Text("Revert after")
        } footer: {
            Text("The node applies the change without a reboot and goes back to its previous config by itself unless you keep it.")
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        preview = await .from { try await client.previewMachineConfig(node: node, base: base, draft: draft) }
    }

    /// Applying needs a fresh Face ID / passcode when the app lock is on, like revealing secrets.
    private func startTry() async {
        message = nil
        if model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Try a config change on \(hostname)")) {
            message = failure
            return
        }
        trying = true
    }
}

/// A unified diff: added lines on green, removed ones on red, each with its sign so the
/// colour is not the only cue.
private struct ConfigDiffView: View {
    let lines: [ConfigDiffLine]

    @ScaledMetric(relativeTo: .caption) private var rowHeight: CGFloat = 18
    @State private var width: CGFloat = 0

    var body: some View {
        ScrollView(.horizontal) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(lines) { line in
                    HStack(spacing: 6) {
                        Text(verbatim: sign(line.kind)).frame(width: 10)
                        Text(verbatim: line.text)
                    }
                    .font(.caption.monospaced())
                    .foregroundStyle(line.kind == .hunk ? Color.secondary : Color.primary)
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.horizontal, 8)
                    .frame(height: rowHeight)
                }
            }
            .frame(minWidth: width, alignment: .leading)
            // Behind the lines, so the colours span the widest one.
            .background {
                VStack(spacing: 0) {
                    ForEach(lines) { line in
                        tint(line.kind).frame(height: rowHeight)
                    }
                }
            }
            .padding(.vertical, 6)
        }
        .background {
            GeometryReader { proxy in
                Color.clear
                    .onAppear { width = proxy.size.width }
                    .onChange(of: proxy.size.width) { _, new in width = new }
            }
        }
    }

    private func sign(_ kind: ConfigDiffLine.Kind) -> String {
        switch kind {
        case .added: "+"
        case .removed: "-"
        case .hunk, .context: " "
        }
    }

    private func tint(_ kind: ConfigDiffLine.Kind) -> Color {
        switch kind {
        case .added: Color.statusOK.opacity(0.18)
        case .removed: Color.statusBad.opacity(0.18)
        case .hunk, .context: Color.clear
        }
    }
}
