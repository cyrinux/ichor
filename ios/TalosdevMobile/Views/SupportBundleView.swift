import SwiftUI
import TalosdevMobileCore
import UniformTypeIdentifiers

/// A support bundle handed to fileExporter; the wrapper reads the file lazily, not in memory.
struct SupportBundleDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.zip] }
    let url: URL

    init(url: URL) { self.url = url }

    init(configuration: ReadConfiguration) throws {
        throw CocoaError(.featureUnsupported) // export only
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        try FileWrapper(url: url, options: [])
    }
}

/// `talosctl support` (any role; what the role cannot read is left out): pick the nodes, collect their logs and state into a zip
/// kept on the phone, then share it, save it to Files or delete it.
struct SupportBundleView: View {
    @Environment(AppModel.self) private var model
    @State private var nodes: LoadState<[NodeOverview]> = .loading
    @State private var selected: Set<String> = []
    @State private var job = SupportBundleJob()
    @State private var files: [SupportBundleFile] = []
    @State private var directory: URL?
    @State private var message: String?

    var body: some View {
        Group {
            if !model.allows(.supportBundle) {
                List { RoleNotice(feature: .supportBundle, roles: model.activeSummary?.roles ?? []) }
            } else if let notice = model.clusterSupport(.supportBundle).notice {
                VersionNoticeView(notice: notice, detail: model.clusterSupport(.supportBundle).reason)
            } else {
                LoadStateView(state: nodes, retry: loadNodes) { list in content(list) }
            }
        }
        .themedBackground()
        .navigationTitle("Support bundle")
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadNodes() }
        .onAppear { loadFiles() }
        // A finished (or cancelled) collection changes what is on the phone.
        .onChange(of: job.state) { loadFiles() }
        .onDisappear { job.cancel() }
    }

    private func content(_ list: [NodeOverview]) -> some View {
        List {
            Section {
                Label("The bundle contains logs and cluster details. It is NOT masked by screenshot mode: review it before sharing.",
                      systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.orange)
                Text("Parts your talosconfig role cannot read are left out and noted in the bundle (machine config needs os:admin, etcd status os:operator).")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            switch job.state {
            case .running:
                progressSection(list)
            default:
                selectionSection(list)
            }
            if !files.isEmpty { filesSection }
        }
    }

    @ViewBuilder
    private func selectionSection(_ list: [NodeOverview]) -> some View {
        let all = list.filter(\.reachable).map(\.node)
        let chosen = supportSelection(all)
        let allChosen = chosen.count == all.count
        Section {
            ForEach(list) { node in
                Button { toggle(node.node) } label: {
                    HStack {
                        Image(systemName: selected.contains(node.node) ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(selected.contains(node.node) ? Color.accentColor : Color.secondary)
                        VStack(alignment: .leading) {
                            Text(verbatim: node.hostname).foregroundStyle(.primary)
                            Text(verbatim: node.reachable ? node.node : "\(node.node)  ·  \(NodeHealth.unreachable.label)")
                                .font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                }
                .disabled(!node.reachable)
            }
        } header: {
            HStack {
                Text("Nodes") + Text(verbatim: " (\(chosen.count) / \(all.count))")
                Spacer()
                Button(allChosen ? String(localized: "Select none") : String(localized: "Select all")) {
                    selected = allChosen ? [] : Set(all)
                }
                .font(.caption)
                .textCase(nil)
            }
        }
        Section {
            Button("Create bundle") { Task { await start(all) } }
                .disabled(chosen.isEmpty)
            outcome
            if let message { Text(message).font(.footnote).foregroundStyle(.red) }
        } footer: {
            Text("Collecting takes a few minutes per node. Keep the app open: the screen stays on meanwhile.")
        }
    }

    @ViewBuilder
    private var outcome: some View {
        switch job.state {
        case .finished(let file, let size):
            Label {
                Text("\(file.lastPathComponent) is ready (\(formatBytes(size)))")
            } icon: {
                Image(systemName: "checkmark.circle.fill")
            }
            .font(.footnote)
            .foregroundStyle(.green)
        case .failed(let error):
            if versionNotice(error) != nil {
                ErrorOrNoticeText(message: error)
            } else {
                Text("Bundle failed: \(error)").font(.footnote).foregroundStyle(.red)
            }
        case .cancelled:
            Text("The collection was interrupted").font(.footnote).foregroundStyle(.secondary)
        case .idle, .running:
            EmptyView()
        }
    }

    /// The share of finished rows, where each node stands, then the cluster-wide part.
    @ViewBuilder
    private func progressSection(_ list: [NodeOverview]) -> some View {
        let hostnames = Dictionary(list.map { ($0.node, $0.hostname) }, uniquingKeysWith: { first, _ in first })
        let progress = job.progress
        Section {
            ProgressView(value: progress.fractionDone(nodes: job.nodes))
            ForEach(job.nodes, id: \.self) { node in
                SupportNodeRow(name: hostnames[node] ?? node, state: progress.state(of: node))
            }
            SupportNodeRow(name: String(localized: "Cluster"), state: progress.state(of: ""))
            Button("Cancel", role: .cancel) { job.cancel() }
        } header: {
            Text("Collecting…")
        }
    }

    private var filesSection: some View {
        Section {
            ForEach(files) { file in
                if let url = directory?.appendingPathComponent(file.name) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(verbatim: file.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                        Text(verbatim: "\(file.modified.formatted(date: .abbreviated, time: .shortened)) · \(formatBytes(file.size))")
                            .font(.caption).foregroundStyle(.secondary)
                        SupportBundleActions(url: url, onDelete: loadFiles)
                    }
                }
            }
        } header: {
            Text("Saved bundles")
        } footer: {
            Text("Bundles are kept on this phone only, out of backups: delete them when done.")
        }
    }

    /// The selection limited to the nodes that can be asked.
    private func supportSelection(_ all: [String]) -> [String] {
        all.filter(selected.contains)
    }

    private func toggle(_ node: String) {
        if selected.contains(node) { selected.remove(node) } else { selected.insert(node) }
    }

    /// Every reachable node is selected by default.
    private func loadNodes() async {
        guard let client = model.client, model.allows(.supportBundle) else { return }
        nodes = await .from { try await client.overview().nodes }
        if case .loaded(let list, _) = nodes {
            if selected.isEmpty { selected = Set(list.filter(\.reachable).map(\.node)) }
            await model.loadFeatures(of: list)
        }
    }

    private func loadFiles() {
        directory = try? SupportBundleStore.directory()
        files = (try? SupportBundleStore.list()) ?? []
    }

    /// Face ID / passcode first when the app lock is on; the file list reloads when done.
    private func start(_ all: [String]) async {
        guard let client = model.client else { return }
        message = nil
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Create a support bundle")) {
            message = failure
            return
        }
        job.start(client: client, context: model.activeContext, nodes: supportSelection(all))
    }
}

/// One node of a running collection: waiting, its current step with "3 / 8", or done.
private struct SupportNodeRow: View {
    let name: String
    let state: SupportNodeState

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: name).font(.subheadline)
                Spacer()
                switch state {
                case .waiting:
                    Text("waiting").font(.caption).foregroundStyle(.secondary)
                case .collecting(let progress):
                    if progress.total > 0 {
                        Text(verbatim: "\(progress.done) / \(progress.total)").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                    }
                case .done:
                    Text("done").font(.caption).foregroundStyle(.secondary)
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                }
            }
            if case .collecting(let progress) = state {
                if let fraction = progress.fraction {
                    ProgressView(value: fraction)
                } else {
                    ProgressView().frame(maxWidth: .infinity, alignment: .leading)
                }
                if !progress.step.isEmpty {
                    Text(verbatim: progress.step).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
    }
}

/// Share, Save to Files and Delete for a support bundle.
private struct SupportBundleActions: View {
    let url: URL
    let onDelete: () -> Void

    @State private var exporting = false
    @State private var confirmDelete = false
    @State private var message: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                ShareLink(item: url) { Label("Share", systemImage: "square.and.arrow.up") }
                Spacer()
                Button { exporting = true } label: { Label("Save to Files", systemImage: "folder") }
                Spacer()
                Button(role: .destructive) { confirmDelete = true } label: { Label("Delete", systemImage: "trash") }
            }
            .buttonStyle(.bordered)
            .labelStyle(.iconOnly)
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        }
        .fileExporter(isPresented: $exporting, document: SupportBundleDocument(url: url), contentType: .zip,
                      defaultFilename: url.lastPathComponent) { result in
            switch result {
            case .success: message = String(localized: "Saved.")
            case .failure(let error): message = error.localizedDescription
            }
        }
        .confirmationDialog(Text("Delete this bundle?"), isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) {
                do {
                    try SupportBundleStore.delete(url)
                    onDelete()
                } catch {
                    message = error.localizedDescription
                }
            }
        }
    }
}
