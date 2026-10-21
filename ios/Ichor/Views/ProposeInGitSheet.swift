import SwiftUI
import IchorCore

/// "Propose in Git" for a machine config change, shown only when the cluster has a repository:
/// the sheet that writes the change from base to draft (both redacted) into the node's file.
struct ProposeInGitButton: View {
    let base: String
    let draft: String
    let hostname: String
    var label: LocalizedStringKey = "Propose in Git"

    @Environment(AppModel.self) private var model
    @State private var proposing = false

    var body: some View {
        if model.activeClusterRepo != nil {
            VStack(spacing: 6) {
                Button { proposing = true } label: {
                    Text(label).frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
                Text("Writes this change into the node's file of the cluster repository, as a pull request.")
                    .font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            .sheet(isPresented: $proposing) {
                if let repo = model.activeClusterRepo {
                    ProposeInGitSheet(repo: repo, base: base, draft: draft, hostname: hostname)
                }
            }
        }
    }
}

/// The values a change sets, the node's file they go into (or the new one), the values a shared
/// patch sets left out; Review opens the editor's review of that file with them written, then
/// the pull request.
struct ProposeInGitSheet: View {
    let repo: ClusterRepo
    let base: String
    let draft: String
    let hostname: String
    /// The node's role, for talconfig.yaml's matching; nil reads it from base (the same node's).
    var role: String?

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<WriteBackPlanned> = .loading

    /// The values, and where they go.
    struct WriteBackPlanned: Equatable, Sendable {
        let patch: TalosConfigPatch
        let plan: TalosWriteBackPlan
    }

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: load) { planned in
                List {
                    Section("Changed values") {
                        ForEach(planned.patch.changes) { change in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: change.path.isEmpty ? change.kind : change.path).font(.callout.monospaced())
                                if change.removed {
                                    Text("Removed on the node: remove it from the repository by hand.").font(.caption).foregroundStyle(.secondary)
                                } else {
                                    Text(verbatim: "\(change.from?.display ?? "—") → \(change.to?.display ?? "—")")
                                        .font(.caption.monospaced()).foregroundStyle(.secondary)
                                }
                            }
                        }
                        ForEach(planned.patch.notes, id: \.self) { Text(verbatim: $0).font(.caption).foregroundStyle(.secondary) }
                    }
                    Section {
                        Text(planned.plan.create ? "Creates \(planned.plan.file): the repository has no file for this node."
                                                 : "Written into \(planned.plan.file)")
                        ForEach(planned.plan.skipped, id: \.path) { skip in
                            SectionError(message: String(localized: "\(skip.path) skipped: \(skip.why)"))
                        }
                        if planned.plan.patch.isEmpty {
                            Text("Every changed value is set by a shared patch: edit those files instead.").foregroundStyle(.secondary)
                        } else {
                            NavigationLink {
                                RepoEditView(repo: repo, path: planned.plan.file,
                                             quickEdit: .talos(hostname: hostname, plan: planned.plan, firstPath: planned.patch.firstPath))
                            } label: {
                                Text("Review the pull request")
                            }
                        }
                    }
                }
            }
            .themedBackground()
            .navigationTitle("Propose in Git")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
            }
        }
        .task { await load() }
    }

    private func load() async {
        do {
            let patch = try await TalosClient.talosConfigPatchFrom(base: base, draft: draft)
            guard !patch.touchesSecrets else {
                state = .failed(String(localized: "This change touches a secret: secrets are never written to the repository."))
                return
            }
            let plan = try await TalosClient.talosWriteBackPlan(repo, hostname: hostname, role: role ?? configRole(base), patch: patch.patch,
                                                                hosts: hosts, demo: demo)
            state = .loaded(WriteBackPlanned(patch: patch, plan: plan), at: Date())
        } catch {
            state = .failed(error.localizedDescription)
        }
    }
}

/// The editor of a file of the cluster repository at its branch head, a quick edit applied:
/// its review, then the pull request.
struct RepoEditView: View {
    let repo: ClusterRepo
    let path: String
    let quickEdit: QuickEdit

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<GitSource> = .loading

    var body: some View {
        Group {
            if case .loaded(let source, _, _) = state {
                SourceEditView(source: source, path: path, app: .repository, quickEdit: quickEdit)
            } else {
                LoadStateView(state: state, retry: load) { _ in EmptyView() }
            }
        }
        .task { await load() }
    }

    private func load() async {
        do {
            let info = try await TalosClient.repoInfo(repo, hosts: hosts, demo: model.activeSummary?.demo == true)
            state = .loaded(repoSource(repo, info: info), at: Date())
        } catch {
            state = .failed(error.localizedDescription)
        }
    }
}
