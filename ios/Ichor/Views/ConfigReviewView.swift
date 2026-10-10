import SwiftUI
import IchorCore

/// What applying the draft changes, validated by the node with a dry run, then the try: the
/// node applies it and reverts by itself unless the change is kept (os:admin).
struct ConfigReviewView: View {
    let node: String
    let hostname: String
    let base: String
    let draft: String
    /// A try changed (or may have changed) the node and is over: its config is to be read again.
    let onFinished: () -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var preview: LoadState<ConfigPreview> = .loading
    @State private var timeout = 300
    @State private var confirming = false
    @State private var trying = false
    @State private var failed = false
    @State private var message: String?
    /// A mode picked, waiting for its confirmation (typed hostname for a reboot).
    @State private var confirmingApply: ConfigApplyMode?
    @State private var applying: ConfigApplyMode?

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
        .confirmationDialog(Text("Apply the change to \(hostname)?"),
                            isPresented: Binding(get: { confirmingApply != nil && confirmingApply != .reboot },
                                                 set: { if !$0 { confirmingApply = nil } }),
                            titleVisibility: .visible, presenting: confirmingApply) { mode in
            Button(modeTitle(mode)) { Task { await startApply(mode) } }
            Button("Cancel", role: .cancel) {}
        } message: { mode in
            if mode == .staged {
                Text("The change is saved on the node and takes effect at its next reboot.")
            } else {
                Text("The change is applied now and stays: nothing reverts it.")
            }
        }
        // Never a surprise reboot: the hostname is typed, as for a reboot.
        .sheet(isPresented: Binding(get: { confirmingApply == .reboot }, set: { if !$0 { confirmingApply = nil } })) {
            HostnameConfirmationSheet(
                title: String(localized: "Apply and reboot \(hostname)?"),
                message: String(localized: "The node applies the change and reboots now. Its pods stop while it reboots."),
                hostname: hostname,
                actionTitle: modeTitle(.reboot)
            ) {
                confirmingApply = nil
                Task { await startApply(.reboot) }
            }
        }
        .fullScreenCover(item: $applying, onDismiss: tryClosed) { mode in
            ConfigApplyView(node: node, hostname: hostname, base: base, draft: draft, mode: mode) { didFail in
                if didFail { failed = true }
            }
        }
        .fullScreenCover(isPresented: $trying, onDismiss: tryClosed) {
            ConfigTryView(node: node, hostname: hostname, base: base, draft: draft, timeoutSeconds: timeout) { outcome in
                if case .failed = outcome { failed = true }
            }
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
                applySection(preview)
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

    private func applySection(_ preview: ConfigPreview) -> some View {
        Section {
            ForEach(preview.applyModes) { mode in
                VStack(alignment: .leading, spacing: 4) {
                    Button(modeTitle(mode)) { confirmingApply = mode }
                    Text(modeDetails(mode)).font(.caption).foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Or apply it for good:")
        }
    }

    private func modeDetails(_ mode: ConfigApplyMode) -> String {
        switch mode {
        case .auto: String(localized: "No reboot, and nothing reverts it.")
        case .staged: String(localized: "Saved on the node; it takes effect when the node next reboots.")
        case .reboot: String(localized: "The node reboots into the new config right away.")
        }
    }

    /// Like a try: a fresh Face ID / passcode first when the app lock is on.
    private func startApply(_ mode: ConfigApplyMode) async {
        message = nil
        if model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Apply the machine config of \(hostname)")) {
            message = failure
            return
        }
        applying = mode
    }

    private func load() async {
        guard let client = model.client else { return }
        preview = await .from { try await client.previewMachineConfig(node: node, base: base, draft: draft) }
    }

    /// Applying needs a fresh Face ID / passcode when the app lock is on, like revealing secrets.
    /// A failed try leaves the draft as it is, to fix or try again; anything else ends the edit.
    private func tryClosed() {
        if failed {
            failed = false
            dismiss()
        } else {
            onFinished()
        }
    }

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
