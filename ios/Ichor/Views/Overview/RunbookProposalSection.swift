import SwiftUI
import IchorCore

/// The run of a proposal that was not kept: it has no runbook id of its own.
let runbookProposalRunID = "ai-proposal"

/// "Turn the fix into a runbook" under a finished answer, then the runbook the model wrote: the
/// planner's plan, the steps the app cannot run, Keep (stored with the cluster's runbooks) and
/// Run (the usual run, which confirms each destructive step and refuses the demo). Nothing is
/// stored until Keep. Same as Android's RunbookProposalCard.kt.
struct RunbookProposalSection: View {
    let report: DiagnosisReport
    let provider: AIProvider
    let answer: String
    /// The navigation path the run screen opens on; nil where the diagnosis has none.
    var path: Binding<[Route]>?
    @Environment(AppModel.self) private var model
    @Environment(AISettings.self) private var ai
    @State private var proposing: Task<Void, Never>?
    @State private var proposal: RunbookProposal?
    @State private var failure: String?
    @State private var kept = false
    @State private var continueIDs: Set<String> = []

    private var job: RunbookJob { RunbookJob.shared }
    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    var body: some View {
        if let proposal {
            proposalSections(proposal)
        } else {
            Section {
                if proposing != nil {
                    HStack {
                        ProgressView()
                        Text("Writing the runbook…")
                    }
                } else {
                    Button {
                        propose()
                    } label: {
                        Label("Turn the fix into a runbook", systemImage: "list.bullet.rectangle")
                    }
                }
                if let failure { Text(verbatim: failure).foregroundStyle(.statusBad) }
            }
            .onDisappear { proposing?.cancel() }
        }
    }

    @ViewBuilder
    private func proposalSections(_ proposal: RunbookProposal) -> some View {
        Section {
            if let plan = proposal.runnablePlan {
                Text(verbatim: plan.name).font(.headline)
            } else {
                Text("No step of the answer is something the app can do.").foregroundStyle(.secondary)
            }
            Label(
                "Written by the model from its answer; checked by the planner; nothing runs until you confirm each step.",
                systemImage: "info.circle"
            )
            .font(.footnote).foregroundStyle(.secondary)
        } header: {
            Text("Runbook from the answer")
        }
        if let plan = proposal.runnablePlan {
            RunbookPlanSections(plan: plan, continueIDs: $continueIDs, editable: true)
            actions(proposal.runbookYAML, plan)
        }
        if !proposal.rejected.isEmpty {
            Section {
                ForEach(proposal.rejected, id: \.self) { Text(verbatim: $0.line).foregroundStyle(.secondary) }
            } header: {
                Text("Not possible from the app")
            }
        }
    }

    private func actions(_ yaml: String, _ plan: RunbookPlan) -> some View {
        Section {
            if kept {
                Text("Kept with this cluster’s runbooks.").font(.footnote).foregroundStyle(.secondary)
            } else {
                Button("Keep") { Task { await keep(yaml) } }
            }
            Button {
                start(yaml, plan)
            } label: {
                Label("Run", systemImage: "play.fill")
            }
            .disabled(!plan.canRun(continueIDs: continueIDs) || job.isRunning)
            if job.isRunning {
                Text("Another runbook is running: open it from its row or its Live Activity.")
                    .font(.footnote).foregroundStyle(.secondary)
            } else if !plan.canRun(continueIDs: continueIDs) {
                Text(
                    "A step the cluster refuses would stop the run: let it through with Continue on error to run the rest."
                )
                .font(.footnote).foregroundStyle(.secondary)
            }
            if let failure { Text(verbatim: failure).foregroundStyle(.statusBad) }
        }
    }

    private func propose() {
        failure = nil
        proposing = Task { @MainActor in
            do {
                let result = try await report.proposeRunbook(
                    provider: provider.id, settings: ai.current, language: appLanguage, answer: answer)
                if !Task.isCancelled { proposal = result }
            } catch {
                if !Task.isCancelled { failure = error.localizedDescription }
            }
            proposing = nil
        }
    }

    private func keep(_ yaml: String) async {
        failure = nil
        do {
            try await RunbookStore.shared.importRunbook(yaml, fingerprint: fingerprint)
            kept = true
        } catch {
            failure = error.localizedDescription
        }
    }

    private func start(_ yaml: String, _ plan: RunbookPlan) {
        guard let client = model.client else { return }
        // The core refuses the demo before any step: its reason shows on the run screen.
        if job.start(
            id: runbookProposalRunID, fingerprint: fingerprint, yaml: yaml, plan: plan, continueIDs: continueIDs,
            client: client)
        {
            path?.wrappedValue.append(.runbookRun(id: runbookProposalRunID))
        }
    }
}
