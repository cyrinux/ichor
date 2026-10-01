import SwiftUI
import TalosdevMobileCore

/// One etcd snapshot download into a private temporary folder, then handed to the user with
/// a file mover. Owned by EtcdView so it survives list scrolling and is cancelled on leave.
@Observable
@MainActor
final class EtcdSnapshotJob {
    struct Transfer: Equatable {
        let hostname: String
        let bytes: Int64
        /// The member's on-disk database size, 0 when unknown.
        let expected: Int64
    }

    struct Saved: Equatable {
        let name: String
        let size: Int64
        let sha256: String
    }

    private(set) var transfer: Transfer?
    private(set) var saved: Saved?
    private(set) var message: String?
    /// The finished snapshot waiting for the file mover.
    private(set) var file: URL?
    var moving = false

    @ObservationIgnored private var pending: Saved?
    private var task: Task<Void, Never>?

    var isBusy: Bool { task != nil || file != nil }

    func start(client: TalosClient, node: String, hostname: String, context: String, expected: Int64, lockEnabled: Bool) {
        guard !isBusy else { return }
        saved = nil
        message = nil
        task = Task {
            await run(client: client, node: node, hostname: hostname, context: context,
                      expected: expected, lockEnabled: lockEnabled)
            task = nil
        }
    }

    func cancel() { task?.cancel() }

    /// The screen went away: stop the transfer and drop any file not handed over yet.
    func leave() {
        cancel()
        if !moving { discard() }
    }

    func moved(_ result: Result<URL, Error>) {
        switch result {
        case .success(let url):
            if let pending {
                saved = Saved(name: url.lastPathComponent, size: pending.size, sha256: pending.sha256)
            }
            message = nil
        case .failure(let error):
            message = error.localizedDescription
        }
        discard() // after a move only the empty temporary folder is left
    }

    func moveCancelled() {
        message = String(localized: "Not saved: the snapshot was deleted from the phone.")
        discard()
    }

    private func run(client: TalosClient, node: String, hostname: String, context: String, expected: Int64, lockEnabled: Bool) async {
        if lockEnabled, let failure = await Authenticator.authenticate(reason: String(localized: "Save an etcd snapshot")) {
            message = failure
            return
        }
        // A folder of its own, so the file keeps its final name for the file mover.
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        } catch {
            message = error.localizedDescription
            return
        }
        let url = folder.appendingPathComponent(etcdSnapshotFilename(context: context, hostname: hostname, date: Date()))
        transfer = Transfer(hostname: hostname, bytes: 0, expected: expected)
        var outcome: SnapshotEvent?
        for await event in client.etcdSnapshot(node: node, destPath: url.path) {
            if case .progress(let bytes) = event {
                transfer = Transfer(hostname: hostname, bytes: bytes, expected: expected)
            } else {
                outcome = event
            }
        }
        transfer = nil

        if !Task.isCancelled, case .finished(_, let size, let sha256)? = outcome {
            pending = Saved(name: url.lastPathComponent, size: size, sha256: sha256)
            file = url
            moving = true
            return
        }
        try? FileManager.default.removeItem(at: folder)
        if case .failed(let error)? = outcome, !Task.isCancelled {
            message = error
        } else {
            message = String(localized: "Snapshot cancelled.")
        }
    }

    private func discard() {
        if let file { try? FileManager.default.removeItem(at: file.deletingLastPathComponent()) }
        file = nil
        pending = nil
        moving = false
    }
}

/// "Save snapshot…" in the etcd screen (os:operator, os:admin or os:etcd:backup).
struct EtcdSnapshotSection: View {
    let etcd: EtcdOverview
    let hostnames: [String: String]
    let job: EtcdSnapshotJob

    @Environment(AppModel.self) private var model
    @State private var selected: String?
    @State private var confirming = false

    private var candidates: [EtcdNodeStatus] { etcd.statuses.filter { $0.error == nil && !$0.memberId.isEmpty } }

    private var target: EtcdNodeStatus? {
        candidates.first { $0.node == selected } ?? defaultSnapshotMember(etcd.statuses)
    }

    var body: some View {
        Section {
            if let transfer = job.transfer {
                progressView(transfer)
            } else {
                if candidates.count > 1 {
                    Picker("Member", selection: Binding(get: { target?.node ?? "" }, set: { selected = $0 })) {
                        ForEach(candidates) { status in
                            Text(verbatim: name(of: status)).tag(status.node)
                        }
                    }
                }
                Button("Save snapshot…") { confirming = true }
                    .disabled(target == nil || job.isBusy)
                    .confirmationDialog(Text("Save an etcd snapshot?"), isPresented: $confirming, titleVisibility: .visible) {
                        Button("Save snapshot") { start() }
                    } message: {
                        Text("The snapshot contains every Kubernetes Secret in clear. Anyone with the file can read them: keep it somewhere safe and delete it when no longer needed.")
                    }
            }
            if let message = job.message {
                Text(message).font(.footnote).foregroundStyle(.secondary)
            }
            if let saved = job.saved {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Saved \(saved.name)").font(.footnote)
                    LabeledContent("Size", value: formatBytes(saved.size)).font(.caption)
                    Text(verbatim: "SHA-256").font(.caption).foregroundStyle(.secondary)
                    Text(verbatim: saved.sha256).font(.caption2.monospaced()).textSelection(.enabled)
                }
            }
        } header: {
            Text("Backup")
        } footer: {
            Text("Saves a snapshot of the etcd database to a file you choose, to recover the cluster with talosctl bootstrap --recover-from.")
        }
    }

    @ViewBuilder private func progressView(_ transfer: EtcdSnapshotJob.Transfer) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Downloading snapshot from \(transfer.hostname)…").font(.subheadline)
            if let fraction = snapshotFraction(bytes: transfer.bytes, expected: transfer.expected) {
                ProgressView(value: fraction)
                Text(verbatim: "\(formatBytes(transfer.bytes)) / ~\(formatBytes(transfer.expected))")
                    .font(.caption).monospacedDigit().foregroundStyle(.secondary)
            } else {
                ProgressView()
                Text(verbatim: formatBytes(transfer.bytes)).font(.caption).monospacedDigit().foregroundStyle(.secondary)
            }
        }
        Button("Cancel", role: .cancel) { job.cancel() }
    }

    private func name(of status: EtcdNodeStatus) -> String {
        let hostname = hostnames[status.memberId] ?? status.node
        return status.isLeader ? "\(hostname) (\(String(localized: "Leader")))" : hostname
    }

    private func start() {
        guard let client = model.client, let target else { return }
        job.start(client: client, node: target.node, hostname: hostnames[target.memberId] ?? target.node,
                  context: model.activeContext, expected: target.dbSize, lockEnabled: model.lock.enabled)
    }
}
