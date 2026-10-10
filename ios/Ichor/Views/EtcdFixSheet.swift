import SwiftUI
import IchorCore

/// The one-tap NOSPACE fix run. Go writes the snapshot (when one is taken) into a private
/// temporary folder; it is handed to the user with a file mover once the run ends. Owned by
/// EtcdView and cancelled when it goes away.
@Observable
@MainActor
final class EtcdFixJob {
    private(set) var events: [EtcdFixProgress] = []
    private(set) var running = false
    private(set) var finished = false
    private(set) var error: String?
    private(set) var savedName: String?
    /// The snapshot waiting for the file mover.
    private(set) var file: URL?
    var moving = false

    private var task: Task<Void, Never>?

    var idle: Bool { !running && !finished }
    var timeline: [TimelineStep<EtcdFixPhase>] {
        etcdFixTimeline(events, finished: finished && error == nil, failure: finished ? error : nil)
    }
    var members: [EtcdFixMember] { events.last?.members ?? [] }

    /// `snapshot`: the member, its hostname and the protection; nil skips the snapshot.
    func start(client: TalosClient, context: String, snapshot: (node: String, hostname: String, encryption: SnapshotEncryption)?,
               lockEnabled: Bool) {
        guard idle else { return }
        running = true
        task = Task {
            await run(client: client, context: context, snapshot: snapshot, lockEnabled: lockEnabled)
            running = false
            finished = true
            task = nil
        }
    }

    func cancel() { task?.cancel() }

    func dismiss() {
        guard !running, !moving else { return }
        events = []
        finished = false
        error = nil
        savedName = nil
    }

    /// The screen went away: stop the run and drop a snapshot not handed over yet.
    func leave() {
        cancel()
        if !moving { discard() }
    }

    func moved(_ result: Result<URL, Error>) {
        switch result {
        case .success(let url): savedName = url.lastPathComponent
        case .failure(let failure): error = error ?? failure.localizedDescription
        }
        discard()
    }

    func moveCancelled() {
        error = error ?? String(localized: "Not saved: the snapshot was deleted from the phone.")
        discard()
    }

    private func run(client: TalosClient, context: String, snapshot: (node: String, hostname: String, encryption: SnapshotEncryption)?,
                     lockEnabled: Bool) async {
        if lockEnabled, let failure = await Authenticator.authenticate(reason: String(localized: "Fix the etcd NOSPACE alarm")) {
            error = failure
            return
        }
        var url: URL?
        if let snapshot {
            let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
            do {
                try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            } catch {
                self.error = error.localizedDescription
                return
            }
            url = folder.appendingPathComponent(etcdSnapshotFilename(context: context, hostname: snapshot.hostname, date: Date(),
                                                                     encrypted: snapshot.encryption.mode != .none))
        }
        var outcome: String??
        for await event in client.etcdNospaceFix(snapshotNode: snapshot?.node ?? "", destPath: url?.path ?? "",
                                                 encryption: snapshot?.encryption ?? .none) {
            switch event {
            case .progress(let progress): events.append(progress)
            case .done(let failure): outcome = .some(failure)
            }
        }
        switch outcome {
        case .some(let failure): error = failure
        case .none: error = String(localized: "Stopped. A defragmentation under way finishes on its member.")
        }
        // A snapshot taken before a later step failed is still worth keeping.
        if let url, FileManager.default.fileExists(atPath: url.path) {
            file = url
            moving = true
        } else if let url {
            try? FileManager.default.removeItem(at: url.deletingLastPathComponent())
        }
    }

    private func discard() {
        if let file { try? FileManager.default.removeItem(at: file.deletingLastPathComponent()) }
        file = nil
        moving = false
    }
}

/// The confirmation of the fix: the defragmentation order, "take a snapshot first" (on by
/// default), then the snapshot's protection.
struct EtcdFixConfirmSheet: View {
    let etcd: EtcdOverview
    let hostnames: [String: String]
    let savedKeys: String
    /// nil encryption: no snapshot.
    let onConfirm: (_ snapshot: (node: String, hostname: String, encryption: SnapshotEncryption)?) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var takeSnapshot = true
    @State private var protecting = false

    private var target: EtcdNodeStatus? { defaultSnapshotMember(etcd.statuses) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    let order = defragOrder(etcd.statuses).map { hostnames[$0.memberId] ?? $0.node }.joined(separator: " → ")
                    Text("Ichor defragments every etcd member, one at a time (\(order)), disarms the alarm, then reads etcd again. Each member is busy while it is defragmented.")
                }
                if target != nil {
                    Section {
                        Toggle(isOn: $takeSnapshot) {
                            VStack(alignment: .leading) {
                                Text("Take a snapshot first")
                                Text("Saved where you choose, protected as you choose, before anything changes.")
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                Section {
                    Button("Fix", role: .destructive) {
                        if takeSnapshot && target != nil { protecting = true } else { onConfirm(nil) }
                    }
                }
            }
            .navigationTitle(String(localized: "Fix the NOSPACE alarm?"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .sheet(isPresented: $protecting) {
                if let target {
                    let hostname = hostnames[target.memberId] ?? target.node
                    SnapshotProtectionSheet(hostname: hostname, savedKeys: savedKeys) { encryption in
                        protecting = false
                        onConfirm((target.node, hostname, encryption))
                    }
                }
            }
        }
    }
}

/// The fix's progress in the etcd screen: steps, members and the result.
struct EtcdFixSection: View {
    let job: EtcdFixJob

    var body: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(label: label(step.phase), state: step.state, at: step.at, message: step.message)
            }
            ForEach(job.members) { member in
                LabeledContent {
                    Text(stateText(member)).foregroundStyle(member.memberState == .failed ? Color.statusBad : Color.secondary)
                } label: {
                    Text(verbatim: member.hostname.isEmpty ? member.node : member.hostname)
                }
                .font(.footnote)
            }
            if let savedName = job.savedName {
                Text("Snapshot saved as \(savedName).").font(.footnote)
            }
            if job.running {
                Button("Cancel", role: .cancel) { job.cancel() }
            } else if job.finished {
                if let error = job.error {
                    Text(error).font(.footnote).foregroundStyle(.statusBad)
                } else {
                    Text("The NOSPACE alarm is cleared.").foregroundStyle(.statusOK)
                }
                Button("OK") { job.dismiss() }.disabled(job.moving)
            }
        } header: {
            Text("NOSPACE fix")
        }
    }

    private func label(_ phase: EtcdFixPhase) -> String {
        switch phase {
        case .snapshot: String(localized: "Snapshot")
        case .defrag: String(localized: "Defragment")
        case .disarm: String(localized: "Disarm the alarm")
        case .recheck: String(localized: "Read etcd again")
        }
    }

    private func stateText(_ member: EtcdFixMember) -> String {
        let state = switch member.memberState {
        case .pending: String(localized: "waiting")
        case .running: String(localized: "defragmenting")
        case .done: String(localized: "done")
        case .failed: String(localized: "failed")
        }
        guard member.reclaimedBytes > 0 else { return state }
        return state + " · " + String(localized: "\(formatBytes(member.reclaimedBytes)) freed")
    }
}
