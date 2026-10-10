import SwiftUI
import IchorCore

/// Talos' disaster recovery guide, linked from the confirmation.
private let disasterRecoveryURL = URL(string: "https://www.talos.dev/latest/advanced/disaster-recovery/")!

/// A snapshot picked for a recovery: copied into a private temporary folder (Go needs a path),
/// as picked (an encrypted file stays encrypted: Go decrypts it while uploading).
struct PickedSnapshot: Equatable {
    let url: URL
    let name: String
    let info: SnapshotInfo
}

/// etcd recovery from a snapshot file. Owned by EtcdView and cancelled when it goes away.
@Observable
@MainActor
final class EtcdRecoverJob {
    private(set) var picked: PickedSnapshot?
    private(set) var loading = false
    private(set) var events: [EtcdRecoverProgress] = []
    private(set) var running = false
    private(set) var finished = false
    private(set) var error: String?

    private var task: Task<Void, Never>?

    var idle: Bool { !loading && picked == nil && !running && !finished }
    var timeline: [TimelineStep<EtcdRecoverPhase>] {
        etcdRecoverTimeline(events, encrypted: picked?.info.encrypted == true,
                            finished: finished && error == nil, failure: finished ? error : nil)
    }
    var uploaded: EtcdRecoverProgress? { events.last { $0.phase == EtcdRecoverPhase.uploading.rawValue && $0.total > 0 } }

    /// Copies the file the importer picked and reads what it needs; true when it is ready.
    func pick(_ result: Result<URL, Error>) async -> Bool {
        guard idle else { return false }
        let source: URL
        switch result {
        case .success(let url): source = url
        case .failure(let failure):
            fail(failure.localizedDescription)
            return false
        }
        loading = true
        defer { loading = false }
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let copy = folder.appendingPathComponent("etcd.snapshot")
        do {
            let scoped = source.startAccessingSecurityScopedResource()
            defer { if scoped { source.stopAccessingSecurityScopedResource() } }
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            try FileManager.default.copyItem(at: source, to: copy)
            let info = try await TalosClient.snapshotInspect(path: copy.path)
            picked = PickedSnapshot(url: copy, name: source.lastPathComponent, info: info)
            return true
        } catch {
            try? FileManager.default.removeItem(at: folder)
            fail(error.localizedDescription)
            return false
        }
    }

    /// Recovers on `node`; `identity` and `passphrase` go to Go and are not kept.
    func start(client: TalosClient, node: String, identity: String, passphrase: String, skipHashCheck: Bool, lockEnabled: Bool) {
        guard let picked, !running else { return }
        running = true
        task = Task {
            if lockEnabled, let failure = await Authenticator.authenticate(reason: String(localized: "Recover etcd")) {
                error = failure
            } else {
                var outcome: String??
                for await event in client.etcdRecover(node: node, path: picked.url.path, identity: identity,
                                                      passphrase: passphrase, skipHashCheck: skipHashCheck) {
                    switch event {
                    case .progress(let progress): events.append(progress)
                    case .done(let failure): outcome = .some(failure)
                    }
                }
                switch outcome {
                case .some(let failure): error = failure
                case .none: error = String(localized: "Recovery stopped")
                }
            }
            discardFile()
            running = false
            finished = true
            task = nil
        }
    }

    func cancel() { task?.cancel() }

    /// Forgets a picked file or a finished run.
    func dismiss() {
        guard !running else { return }
        discardFile()
        picked = nil
        events = []
        finished = false
        error = nil
    }

    /// The screen went away: stop the run and delete the copy.
    func leave() {
        cancel()
        if !running { dismiss() }
    }

    private func fail(_ message: String) {
        error = message
        finished = true
    }

    private func discardFile() {
        if let picked { try? FileManager.default.removeItem(at: picked.url.deletingLastPathComponent()) }
    }
}

/// What opens the picked snapshot and the control plane to recover on, then the confirmation
/// (typed cluster name and the acknowledgement).
struct EtcdRecoverSheet: View {
    let picked: PickedSnapshot
    let nodes: [String]
    let hostnames: [String: String]
    let clusterName: String
    let onStart: (_ node: String, _ identity: String, _ passphrase: String, _ skipHashCheck: Bool) -> Void
    let onCancel: () -> Void

    @State private var secret = ""
    @State private var node: String?
    @State private var skipHash = false
    @State private var confirming = false

    // Explicit: the private @State properties make the memberwise init private.
    init(picked: PickedSnapshot, nodes: [String], hostnames: [String: String], clusterName: String,
         onStart: @escaping (_ node: String, _ identity: String, _ passphrase: String, _ skipHashCheck: Bool) -> Void,
         onCancel: @escaping () -> Void) {
        self.picked = picked
        self.nodes = nodes
        self.hostnames = hostnames
        self.clusterName = clusterName
        self.onStart = onStart
        self.onCancel = onCancel
        _node = State(initialValue: nodes.count == 1 ? nodes.first : nil)
    }

    private var opener: SnapshotOpener { picked.info.opener }
    private var ready: Bool {
        guard node != nil else { return false }
        switch opener {
        case .clear: return true
        case .secretKey, .passphrase: return !secret.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        case .unsupported: return false
        }
    }
    private var nodeName: String { node.map { hostnames[$0] ?? $0 } ?? "" }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    LabeledContent(picked.name, value: formatBytes(picked.info.size))
                        .font(.footnote.monospaced())
                    switch opener {
                    case .clear:
                        Text("This snapshot is not encrypted.")
                    case .passphrase:
                        SecureField("Passphrase", text: $secret)
                    case .secretKey:
                        Text("Encrypted for an age key: paste its secret key (AGE-SECRET-KEY-1…). It is not stored.")
                            .font(.footnote).foregroundStyle(.secondary)
                        SecureField("Age secret key", text: $secret)
                            .font(.footnote.monospaced())
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    case .unsupported:
                        Text("This snapshot is encrypted for a YubiKey or an SSH key, which the phone cannot open: decrypt it on a laptop first, or use a passphrase or age-key snapshot.")
                            .foregroundStyle(.statusBad)
                    }
                }
                Section("Control plane to recover on") {
                    Picker("Control plane to recover on", selection: $node) {
                        ForEach(nodes, id: \.self) { Text(verbatim: hostnames[$0] ?? $0).tag(Optional($0)) }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                if opener == .clear {
                    Section {
                        Toggle(isOn: $skipHash) {
                            VStack(alignment: .leading) {
                                Text("Database copied from a member's data directory")
                                Text("Skips the integrity check: such a copy has no hash.").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                Section {
                    Link("Talos disaster recovery guide", destination: disasterRecoveryURL)
                }
            }
            .navigationTitle(Text("Recover etcd from a snapshot"))
            .navigationBarTitleDisplayMode(.inline)
            .themedBackground()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { onCancel() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Continue") { confirming = true }.disabled(!ready)
                }
            }
            .sheet(isPresented: $confirming) {
                HostnameConfirmationSheet(
                    title: String(localized: "Recover etcd on \(nodeName)?"),
                    message: String(localized: "etcd starts again on \(nodeName) from this snapshot, with one member. Writes made after the snapshot are lost."),
                    hostname: clusterName,
                    actionTitle: String(localized: "Recover"),
                    acknowledgments: [String(localized: "I understand the other control planes must be reset before they rejoin")]
                ) {
                    confirming = false
                    guard let node else { return }
                    let key = opener == .secretKey ? secret : ""
                    let pass = opener == .passphrase ? secret : ""
                    secret = ""
                    onStart(node, key, pass, skipHash && opener == .clear)
                }
            }
        }
    }
}

/// The recovery's progress in the etcd screen: steps and the result.
struct EtcdRecoverSection: View {
    let job: EtcdRecoverJob

    var body: some View {
        Section {
            if job.loading {
                HStack { ProgressView(); Text("Reading the snapshot…") }
            }
            if job.running || !job.events.isEmpty {
                ForEach(job.timeline) { step in
                    RunStepRow(label: label(step.phase), state: step.state, at: step.at, message: step.message)
                }
                if let uploaded = job.uploaded {
                    Text("\(formatBytes(uploaded.bytes)) of \(formatBytes(uploaded.total)) uploaded")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }
            if job.running {
                Button("Cancel", role: .cancel) { job.cancel() }
            } else if job.finished {
                if let error = job.error {
                    Text(error).font(.footnote).foregroundStyle(.statusBad)
                } else {
                    Text("etcd runs again with one member. Reset the other control planes so they join it.")
                        .foregroundStyle(.statusOK)
                }
                Button("OK") { job.dismiss() }
            }
        } header: {
            Text("Recover etcd from a snapshot")
        }
    }

    private func label(_ phase: EtcdRecoverPhase) -> String {
        switch phase {
        case .decrypting: String(localized: "Decrypt")
        case .uploading: String(localized: "Upload")
        case .bootstrapping: String(localized: "Bootstrap")
        case .waiting: String(localized: "Wait for etcd")
        }
    }
}
