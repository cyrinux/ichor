import SwiftUI
import IchorCore

/// What the blocks a Garage cluster fails to resync are (a sample of them, looked up node by
/// node): the objects they back, live or deleted, and a repair for the metadata that keeps
/// them failing (os:admin).
struct GarageBlockErrorsView: View {
    let instance: GarageInstance

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<GarageBlockReport> = .loading
    @State private var confirmRepair = false
    @State private var repairing = false
    @State private var repairSummary: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            List {
                Section {
                    VerdictRow(verdict: report.verdict)
                    if report.repairsRunning {
                        Label("A metadata repair is running: let it finish, then reload.", systemImage: "gearshape.2")
                            .font(.callout)
                            .foregroundStyle(.statusWarn)
                    }
                }
                Section("Summary") { SummaryRows(report: report) }
                Section {
                    Button {
                        confirmRepair = true
                    } label: {
                        HStack {
                            Label("Repair", systemImage: "wrench.and.screwdriver")
                            Spacer()
                            if repairing { ProgressView() }
                        }
                    }
                    .disabled(report.errored == 0 || repairing)
                } footer: {
                    Text("Fixes the block references and reference counts when they are off, and retries the resync of the blocks nothing references any more.")
                }
                ForEach(report.nodes) { NodeSection(node: $0) }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        .navigationTitle(Text("Block errors"))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(Text("Repair the failing blocks?"), isPresented: $confirmRepair, titleVisibility: .visible) {
            Button("Repair") { Task { await repair() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Launches a block-refs and a block-rc metadata repair on every Garage node when needed (not while one runs, nor with a node unreachable), and retries the resync of unreferenced blocks. Repairs run in the background and add disk and network load for a while.")
        }
        .alert(Text("Repair"),
               isPresented: $repairSummary.isPresent(),
               presenting: repairSummary) { _ in
            Button("OK") {}
        } message: { summary in
            Text(verbatim: summary)
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<GarageBlockReport> = await .from { try await client.garageBlockErrors(instance) }
        state = state.refreshed(with: loaded)
    }

    private func repair() async {
        guard let client = model.client, !repairing else { return }
        repairing = true
        defer { repairing = false }
        do {
            let result = try await client.garageRepairBlocks(instance)
            repairSummary = result.outcomes.map(outcomeText).joined(separator: "\n")
            await load()
        } catch {
            repairSummary = String(localized: "Could not repair: \(error.localizedDescription)")
        }
    }
}

private func outcomeText(_ outcome: GarageRepairResult.Outcome) -> String {
    switch outcome {
    case .launched(let repairs): String(localized: "Metadata repairs launched: \(repairs.joined(separator: ", "))")
    case .alreadyRunning: String(localized: "A metadata repair is already running: not launched again.")
    case .unreachable: String(localized: "A node is unreachable: metadata repairs not launched.")
    case .retried(let count): String(localized: "Resyncs retried: \(count)")
    case .error(let message): message
    case .nothingToDo: String(localized: "Nothing to repair.")
    }
}

private struct VerdictRow: View {
    let verdict: GarageBlockReport.Verdict

    var body: some View {
        switch verdict {
        case .clean:
            Label("No block is failing to resync.", systemImage: "checkmark.circle")
                .foregroundStyle(.statusOK)
        case .liveAffected:
            Label("Some failing blocks back live objects: check those objects through S3 before declaring data loss.",
                  systemImage: "exclamationmark.triangle")
                .foregroundStyle(.statusBad)
        case .deletedOnly:
            Label("No live object affected: these blocks only hold deleted data.", systemImage: "info.circle")
                .foregroundStyle(.secondary)
        }
    }
}

private struct SummaryRows: View {
    let report: GarageBlockReport

    var body: some View {
        LabeledContent("Blocks failing to resync", value: report.errored.formatted())
        LabeledContent("Looked up", value: "\(report.detailed.formatted())/\(report.errored.formatted())")
        LabeledContent("Backing live objects") {
            Text(verbatim: report.live.formatted()).foregroundStyle(report.live > 0 ? .red : .secondary)
        }
        LabeledContent("Deleted data only", value: report.cleanupOnly.formatted())
        if report.staleRefs > 0 { LabeledContent("Stale references", value: report.staleRefs.formatted()) }
        if report.refcountMismatches > 0 { LabeledContent("Reference counts off", value: report.refcountMismatches.formatted()) }
        if report.retryable > 0 { LabeledContent("Unreferenced, retryable", value: report.retryable.formatted()) }
    }
}

private struct NodeSection: View {
    let node: GarageBlockNode

    var body: some View {
        Section {
            if !node.error.isEmpty {
                Text("Could not read: \(node.error)").font(.caption).foregroundStyle(.statusBad)
            }
            ForEach(node.blocks) { BlockRow(block: $0) }
        } header: {
            HStack {
                Text(verbatim: node.label).textCase(nil)
                Spacer()
                if node.error.isEmpty {
                    Text("\(node.errored) failing").textCase(nil)
                }
            }
        }
    }
}

private struct BlockRow: View {
    let block: GarageBlock

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: block.shortHash).font(.subheadline.monospaced())
                Spacer()
                ImpactLabel(impact: block.impact)
            }
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary).monospacedDigit()
            if !block.error.isEmpty {
                Text(verbatim: block.error).font(.caption).foregroundStyle(.statusBad)
            }
            ForEach(Array(block.refs.enumerated()), id: \.offset) { _, ref in
                HStack(alignment: .firstTextBaseline) {
                    Image(systemName: ref.live ? "doc" : "trash").foregroundStyle(ref.live ? .red : .secondary)
                    Text(verbatim: ref.label).lineLimit(2).truncationMode(.middle)
                    Spacer()
                    if ref.live { Text("live").foregroundStyle(.statusBad) } else { Text("deleted").foregroundStyle(.secondary) }
                }
                .font(.caption)
            }
        }
    }

    private var details: String {
        var parts = [String(localized: "\(block.errors) attempts")]
        if block.nextTrySecs >= 0 { parts.append(String(localized: "next try in \(localizedDuration(block.nextTrySecs))")) }
        if block.staleRef { parts.append(String(localized: "stale reference")) }
        if block.refcountMismatch { parts.append(String(localized: "reference count off")) }
        return parts.joined(separator: " · ")
    }
}

private struct ImpactLabel: View {
    let impact: GarageBlockImpact

    var body: some View {
        switch impact {
        case .live: Text("Live object").font(.caption).foregroundStyle(.statusBad)
        case .staleRef: Text("Stale reference").font(.caption).foregroundStyle(.statusWarn)
        case .cleanup: Text("Deleted data").font(.caption).foregroundStyle(.secondary)
        case .unknown: Text("Not looked up").font(.caption).foregroundStyle(.secondary)
        }
    }
}
