import SwiftUI
import UIKit
import IchorCore

/// The pull request of an edited source file: title, commit message and description
/// prefilled by the Go core (GitOpsProposalDefaults), the new branch folded away, the target
/// branch asked when the app follows a tag or a commit, a draft switch, then Open pull
/// request. Then the link, and when the controller applies it. A file changed upstream since
/// it was read is refused by the Git host: Reload reads it again.
struct ProposeSheet: View {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    let base: SourceEditBase
    /// The edited file.
    let content: String
    /// The file changed on this branch since it was read: the editor reads it again.
    let onReload: (String) -> Void
    /// The request is open and its result closed: the editor closes too.
    let onDone: () -> Void
    /// The title and commit message of a quick edit ("Bump podinfo to 6.7.0"), nil for the Go
    /// core's.
    let defaultTitle: String?

    // Explicit: the private @State properties make the memberwise init private.
    init(source: GitSource, path: String, app: GitOpsAppRef, base: SourceEditBase, content: String, target: String,
         defaultTitle: String? = nil, onReload: @escaping (String) -> Void, onDone: @escaping () -> Void) {
        self.source = source
        self.path = path
        self.app = app
        self.base = base
        self.content = content
        self.onReload = onReload
        self.onDone = onDone
        self.defaultTitle = defaultTitle
        _target = State(initialValue: target)
    }

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var title = ""
    @State private var message = ""
    @State private var details = ""
    /// "" lets the Go core name it.
    @State private var branch = ""
    @State private var target: String
    @State private var draft = false
    @State private var defaultsAsked = false
    @State private var sending = false
    @State private var failure: String?
    @State private var result: GitProposalResult?

    private var demo: Bool { model.activeSummary?.demo == true }

    /// The app follows a tag, a commit or a range: the user names the branch.
    private var asksTarget: Bool { proposalBaseRef(for: source) == nil }

    private var canSend: Bool {
        !sending && [title, message, target].allSatisfy { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    }

    var body: some View {
        NavigationStack {
            Group {
                if let result {
                    ProposalResultView(result: result, app: app)
                } else {
                    form
                }
            }
            .navigationTitle(result == nil ? Text("Open a pull request") : Text("Pull request opened"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if result == nil {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }.disabled(sending)
                    }
                } else {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { onDone() }
                    }
                }
            }
        }
        .interactiveDismissDisabled(sending || result != nil)
        .task { await loadDefaults() }
    }

    private var form: some View {
        Form {
            if let failure { failureSection(failure) }
            if asksTarget {
                Section {
                    Text("This app follows \(source.tracked), which is not a branch. Name the branch the pull request goes to.")
                        .font(.footnote)
                    branchField("Target branch", text: $target)
                } header: {
                    Text("Target branch")
                }
            }
            Section("Title") {
                TextField("Title", text: $title)
            }
            Section("Commit message") {
                TextField("Commit message", text: $message, axis: .vertical).lineLimit(1...4)
            }
            Section("Description") {
                TextField("Description", text: $details, axis: .vertical).lineLimit(3...10)
            }
            Section {
                DisclosureGroup("New branch") {
                    branchField("New branch", text: $branch)
                }
                if !asksTarget {
                    LabeledContent("Target branch") {
                        Text(verbatim: target).font(.callout.monospaced())
                    }
                }
                Toggle("Open as a draft", isOn: $draft)
            }
            Section {
                Button {
                    Task { await send() }
                } label: {
                    HStack {
                        if sending {
                            ProgressView()
                            Text("Opening the pull request…")
                        } else {
                            Text("Open pull request")
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .disabled(!canSend)
            }
        }
        .themedBackground()
        .disabled(sending)
    }

    private func branchField(_ label: LocalizedStringKey, text: Binding<String>) -> some View {
        TextField(label, text: text)
            .font(.callout.monospaced())
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
    }

    private func failureSection(_ failure: String) -> some View {
        Section {
            Label {
                Text(verbatim: failure).textSelection(.enabled)
            } icon: {
                Image(systemName: "exclamationmark.triangle.fill")
            }
            .foregroundStyle(.statusBad)
            if isStaleProposal(failure) {
                Button("Reload") {
                    let branch = target.trimmingCharacters(in: .whitespacesAndNewlines)
                    dismiss()
                    onReload(branch)
                }
            }
        }
    }

    /// The Go core's wording, once; fields typed into meanwhile are kept.
    private func loadDefaults() async {
        guard !defaultsAsked else { return }
        defaultsAsked = true
        if let defaultTitle {
            title = defaultTitle
            message = defaultTitle
        }
        let cluster = model.activeSummary?.name ?? ""
        let loaded: GitProposalDefaults?
        if app.isRepository {
            loaded = try? await TalosClient.repoProposalDefaults(repoURL: source.repoURL, path: path, cluster: cluster)
        } else {
            loaded = try? await TalosClient.proposalDefaults(app: app, path: path, cluster: cluster)
        }
        guard let defaults = loaded else { return }
        if title.isEmpty { title = defaults.title }
        if message.isEmpty { message = defaults.message }
        if details.isEmpty { details = defaults.body }
        if branch.isEmpty { branch = defaults.branch }
    }

    private func send() async {
        guard let client = model.client, canSend else { return }
        sending = true
        defer { sending = false }
        failure = nil
        let baseRef = target.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let sha = try await lockSha(client, baseRef: baseRef)
            let proposal = GitProposal(repoURL: source.repoURL, baseRef: baseRef, path: path, content: content, fileSha: sha,
                                       message: message, branch: branch.trimmingCharacters(in: .whitespacesAndNewlines),
                                       title: title, body: details, draft: draft)
            result = try await client.propose(proposal, on: source, hosts: hosts, demo: demo)
            announce(String(localized: "Pull request opened"))
        } catch {
            failure = error.localizedDescription
        }
    }

    /// The sha the commit names: the one read, or, for another branch than the one read, that
    /// branch's when the file is the same there. A different file keeps the sha read, and the
    /// Git host refuses it as changed since opened (Reload).
    private func lockSha(_ client: TalosClient, baseRef: String) async throws -> String {
        guard baseRef != base.ref else { return base.file.sha }
        let there = try await client.forgeFile(source, path: path, ref: baseRef, hosts: hosts, demo: demo)
        return sameSourceText(there.content, base.file.content) ? there.sha : base.file.sha
    }
}

/// The opened request: its link to open or copy, and when the controller applies it.
private struct ProposalResultView: View {
    let result: GitProposalResult
    let app: GitOpsAppRef

    @State private var copied = false

    var body: some View {
        List {
            Section {
                Label {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Pull request opened").font(.headline)
                        Text(verbatim: result.number > 0 ? "#\(result.number) · \(result.branch)" : result.branch)
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                } icon: {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
                }
                Group {
                    if app.isRepository {
                        Text("Nothing changes on the cluster until it is merged and applied.")
                    } else {
                        Text("\(app.toolLabel) applies it once it is merged, at its next reconcile.")
                    }
                }
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let url = webURL(result.url) {
                Section {
                    Link(destination: url) { Label("Open", systemImage: "safari") }
                    Button { copy(result.url) } label: {
                        Label(copied ? LocalizedStringKey("Copied") : LocalizedStringKey("Copy link"),
                              systemImage: copied ? "checkmark" : "doc.on.doc")
                    }
                }
            }
        }
        .themedBackground()
    }

    private func copy(_ link: String) {
        UIPasteboard.general.string = link
        copied = true
        announce(String(localized: "Copied"))
        Task {
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }
}
