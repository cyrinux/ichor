import SwiftUI
import UIKit
import IchorCore

/// The cluster home's Repository row: shown once a repository is saved for the cluster on
/// screen (the demo has one built in); opens it from the root.
struct ClusterRepoToolLink<Label: View>: View {
    @ViewBuilder let label: () -> Label
    @Environment(AppModel.self) private var model

    var body: some View {
        if let repo = model.activeClusterRepo {
            NavigationLink { ClusterRepoRootView(repo: repo) } label: { label() }
        }
    }
}

/// The cluster's repository at its branch head: asks the forge where the branch stands, then
/// the source browser from the root (files open read-only, Edit proposes a pull request).
/// Without a token for its Git host, says so with a way to add one.
struct ClusterRepoRootView: View {
    let repo: ClusterRepo
    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<GitSource> = .loading
    @State private var missingHost: String?

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        Group {
            if let missingHost {
                GitHostMissingView(host: missingHost, page: repoWebURL(repo.repoURL))
                    .themedBackground()
                    .navigationTitle("Repository")
                    .navigationBarTitleDisplayMode(.inline)
            } else if case .loaded(let source, _, _) = state {
                SourceBrowserView(source: source, dir: "", app: .repository)
            } else {
                LoadStateView(state: state, retry: load) { _ in EmptyView() }
                    .navigationTitle("Repository")
                    .navigationBarTitleDisplayMode(.inline)
            }
        }
        .task(id: repo) { await load() }
        // A token added in Settings (the shortcut): read again on the way back.
        .onChange(of: hosts.hosts) { _, _ in Task { await load() } }
    }

    private func load() async {
        do {
            let info = try await TalosClient.repoInfo(repo, hosts: hosts, demo: demo)
            missingHost = nil
            state = .loaded(repoSource(repo, info: info), at: Date())
        } catch let missing as GitHostMissing {
            missingHost = missing.host
        } catch {
            missingHost = nil
            state = .failed(error.localizedDescription)
        }
    }
}

/// The repository of the cluster `context`: its address, the branch (blank for the default
/// one), the Talos and Kubernetes directories, and Check, which asks the forge with the host's
/// saved token for the default branch and the token's push right. Without a token for the
/// address's host, says so with a way to Settings → Git hosts.
struct ClusterRepoSheet: View {
    let context: ContextSummary
    let label: String

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var url: String
    @State private var branch: String
    @State private var talosDir: String
    @State private var kubernetesDir: String
    @State private var checking = false
    @State private var result: GitRepoInfo?
    @State private var failure: String?
    private let saved: ClusterRepo?

    // Explicit: the private @State makes the memberwise init private.
    init(context: ContextSummary, label: String, saved: ClusterRepo?) {
        self.context = context
        self.label = label
        self.saved = saved
        _url = State(initialValue: saved?.repoURL ?? "")
        _branch = State(initialValue: saved?.branch ?? "")
        _talosDir = State(initialValue: saved?.talosDir ?? "")
        _kubernetesDir = State(initialValue: saved?.kubernetesDir ?? "")
    }

    private var repo: ClusterRepo? {
        normalizeClusterRepo(ClusterRepo(repoURL: url, branch: branch, talosDir: talosDir, kubernetesDir: kubernetesDir))
    }
    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Repository address", text: $url, prompt: Text(verbatim: "https://github.com/example-org/cluster-config"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                    Button("Paste") { url = UIPasteboard.general.string?.trimmingCharacters(in: .whitespacesAndNewlines) ?? url }
                    if let host = repo?.host, !host.isEmpty, !hosts.hasToken(host), !demo {
                        Text("No token is saved for \(host): add it in Settings → Git hosts.")
                            .font(.footnote).foregroundStyle(.statusWarn)
                        NavigationLink {
                            List { GitHostsView() }.navigationTitle("Git hosts")
                        } label: {
                            Text("Open Git hosts")
                        }
                    }
                } footer: {
                    Text("The Git repository that holds \(label)’s configuration. Ichor reads it with the token of its Git host; nothing is cloned.")
                }
                Section {
                    TextField("Branch", text: $branch, prompt: Text("Blank for the default branch"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    TextField("Talos directory", text: $talosDir, prompt: Text(verbatim: "talos"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    TextField("Kubernetes directory", text: $kubernetesDir, prompt: Text(verbatim: "kubernetes"))
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
                Section {
                    Button {
                        Task { await check() }
                    } label: {
                        HStack {
                            if checking { ProgressView() }
                            checking ? Text("Checking…") : Text("Check")
                        }
                    }
                    .disabled(checking || repo == nil)
                    checkLine
                } footer: {
                    Text("Edits are never committed to the branch: each one opens a pull or merge request.")
                }
                // The active cluster only: a proposal is recorded in the active cluster's audit log.
                if model.activeSummary?.fingerprint == context.fingerprint, let active = model.activeClusterRepo {
                    UpdateBotSection(repo: active)
                }
                if saved != nil {
                    Section {
                        Button("Delete", role: .destructive) {
                            model.setClusterRepo(nil, for: context.fingerprint)
                            dismiss()
                        }
                    }
                }
            }
            .navigationTitle("Cluster repository")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        model.setClusterRepo(repo, for: context.fingerprint)
                        dismiss()
                    }
                    .disabled(repo == nil)
                }
            }
            // Another address or branch: the last answer was about something else.
            .onChange(of: url) { resetCheck() }
            .onChange(of: branch) { resetCheck() }
        }
    }

    @ViewBuilder
    private var checkLine: some View {
        if let failure {
            Text("Check failed: \(failure)").font(.footnote).foregroundStyle(.statusBad)
        } else if let result {
            VStack(alignment: .leading, spacing: 4) {
                Label {
                    Text("\(result.crumb) (default branch: \(result.defaultBranch))")
                } icon: {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
                }
                if result.canPush {
                    Text("This token can push: edits open pull requests.").font(.footnote).foregroundStyle(.statusOK)
                } else {
                    Text("This token can read only: pull requests will be refused.").font(.footnote).foregroundStyle(.statusWarn)
                }
            }
        } else {
            Text("Not checked").font(.footnote).foregroundStyle(.secondary)
        }
    }

    private func check() async {
        guard !checking, let repo else { return }
        checking = true
        defer { checking = false }
        failure = nil
        result = nil
        do {
            result = try await TalosClient.repoInfo(repo, hosts: hosts, demo: demo)
        } catch {
            failure = error.localizedDescription
        }
    }

    private func resetCheck() {
        result = nil
        failure = nil
    }
}
