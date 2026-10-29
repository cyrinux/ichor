import SwiftUI
import IchorCore

/// Dependency updates in the Cluster repository sheet: whether a bot is configured on repo (its
/// file named), else a configuration drafted for it to review and propose.
struct UpdateBotSection: View {
    let repo: ClusterRepo

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<Checked> = .loading
    @State private var reviewing = false

    /// The answer, with the source a proposal goes to.
    struct Checked: Hashable, Sendable {
        let source: GitSource
        let config: UpdateBotConfig
    }

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        Section("Dependency updates") {
            switch state {
            case .loading:
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Looking for a dependency-update bot…").foregroundStyle(.secondary)
                }
            case .failed(let message):
                Text("Could not read the repository: \(message)").font(.footnote).foregroundStyle(.statusBad)
                Button("Retry") { Task { await load() } }
            case .loaded(let checked, _, _):
                if checked.config.configured {
                    Label {
                        Text("A bot is configured: \(checked.config.present.map(\.path).joined(separator: ", "))")
                    } icon: {
                        Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
                    }
                } else if checked.config.proposable {
                    Text("No update bot in this repository.").foregroundStyle(.secondary)
                    Button("Propose a configuration…") { reviewing = true }
                } else {
                    Text(verbatim: checked.config.notes.first ?? "").font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
        .task(id: repo) { await load() }
        .sheet(isPresented: $reviewing) {
            if case .loaded(let checked, _, _) = state {
                UpdateBotDraftView(source: checked.source, config: checked.config)
            }
        }
    }

    private func load() async {
        state = .loading
        state = await .from {
            let (source, config) = try await TalosClient.updateBotConfig(repo, hosts: hosts, demo: demo)
            return Checked(source: source, config: config)
        }
    }
}

/// The drafted configuration: why each part is there, the file itself (read only), then the
/// pull request that adds it.
private struct UpdateBotDraftView: View {
    let source: GitSource
    let config: UpdateBotConfig

    @Environment(\.dismiss) private var dismiss
    @State private var proposing = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("The app wrote \(config.draft.path) for the files it found. Read it, then open a pull request: nothing changes until you merge it.")
                        .font(.footnote)
                    ForEach(config.notes, id: \.self) { Text(verbatim: "• \($0)").font(.footnote) }
                }
                Section {
                    ScrollView(.horizontal) {
                        Text(verbatim: config.draft.content)
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                            .fixedSize(horizontal: true, vertical: false)
                    }
                } header: {
                    Text(verbatim: config.draft.path)
                }
                Section {
                    Button("Continue to the pull request") { proposing = true }
                }
            }
            .themedBackground()
            .navigationTitle("Add a dependency-update bot configuration")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .sheet(isPresented: $proposing) {
                // A new file: read at the branch it goes to, with no sha, so the commit creates it.
                ProposeSheet(source: source, path: config.draft.path, app: .repository,
                             base: SourceEditBase(file: GitFile(path: config.draft.path), ref: source.tracked),
                             content: config.draft.content, target: source.tracked,
                             defaultTitle: String(localized: "Add a dependency-update bot configuration"),
                             defaultBody: config.proposalBody,
                             onReload: { _ in }, onDone: {
                                 proposing = false
                                 dismiss()
                             })
            }
        }
    }
}
