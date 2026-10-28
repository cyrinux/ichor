import SwiftUI
import IchorCore

/// A runbook's run: each step's state as Go reports it, Stop, and before each destructive step
/// its confirmation (the node's hostname typed for a node step, a plain confirmation for a
/// Kubernetes one); Go holds the step until then. Leaving the screen keeps the run going, with
/// its Live Activity. Once it ended, each step's outcome and a link to the Activity log. Same as
/// Android's RunbookRunScreen.kt.
struct RunbookRunView: View {
    let id: String
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    /// The waiting step whose confirmation was dismissed: asked again only from its row.
    @State private var declined: Int?
    @State private var typing: RunbookPlanStep?
    @State private var asking: RunbookPlanStep?

    private var job: RunbookJob { RunbookJob.shared }
    private var run: RunbookJob.Run? { job.run.flatMap { $0.id == id ? $0 : nil } }

    var body: some View {
        Form {
            if let run {
                header(run)
                Section {
                    ForEach(Array(run.plan.steps.enumerated()), id: \.element.id) { index, step in
                        RunbookStepStateRow(step: step, state: run.progress.steps[safe: index]) {
                            declined = nil
                            ask(step)
                        }
                    }
                }
                footer(run)
            } else {
                Section {
                    Text("This runbook is not running. Open its plan to run it.").foregroundStyle(.secondary)
                }
            }
        }
        .navigationTitle(run.map { Text(verbatim: $0.plan.name) } ?? Text("Runbook"))
        .clusterScreenTitle(run.map { Text(verbatim: $0.plan.name) } ?? Text("Runbook"))
        .toolbar {
            if run?.running == true {
                ToolbarItem(placement: .primaryAction) {
                    Button(role: .destructive) { job.stop() } label: { Text("Stop") }
                        .disabled(run?.stopping == true)
                        .accessibilityLabel(Text("Stop the runbook"))
                }
            }
        }
        .onChange(of: run?.progress.waiting?.index, initial: true) { _, index in
            guard let index, index != declined, let step = run?.plan.steps[safe: index] else { return }
            ask(step)
        }
        .sheet(item: $typing) { step in
            HostnameConfirmationSheet(
                title: String(localized: "Run \(step.id)?"),
                message: String(localized: "\(step.action) on \(step.target.node). Type the node to confirm."),
                hostname: step.target.node,
                actionTitle: String(localized: "Run step"),
                onConfirm: { confirm(step) }
            )
            .onDisappear { if run?.progress.waiting?.id == step.id { declined = run?.progress.waiting?.index } }
        }
        .confirmationDialog(
            asking.map { String(localized: "Run \($0.id)?") } ?? "",
            isPresented: Binding(get: { asking != nil }, set: { if !$0 { decline() } }),
            titleVisibility: .visible,
            presenting: asking
        ) { step in
            Button("Run step", role: .destructive) { confirm(step) }
            Button("Not now", role: .cancel) { decline() }
        } message: { step in
            Text(verbatim: "\(step.action) · \(step.target.label)")
        }
    }

    @ViewBuilder private func header(_ run: RunbookJob.Run) -> some View {
        Section {
            if run.running {
                if let position = run.progress.position {
                    Text("Step \(String(position.step)) of \(String(position.total))").font(.headline)
                }
                if run.progress.waiting != nil {
                    Text("Waiting for you").foregroundStyle(.orange)
                }
                ProgressView(value: Double(run.progress.ended), total: Double(max(run.progress.steps.count, 1)))
            } else {
                Text(verbatim: RunbookJob.result(run))
                    .foregroundStyle(run.succeeded ? Color.green : run.stopped ? Color.secondary : Color.red)
            }
        }
    }

    @ViewBuilder private func footer(_ run: RunbookJob.Run) -> some View {
        if !run.running {
            Section {
                Button {
                    path.append(.activity(cluster: model.activeContext))
                } label: {
                    Label("Activity log", systemImage: "list.bullet.clipboard")
                }
                Button("Done") {
                    job.reset()
                    if path.last == .runbookRun(id: id) { path.removeLast() }
                }
            }
        }
    }

    private func ask(_ step: RunbookPlanStep) {
        switch step.confirmation {
        case .typedHostname?: typing = step
        case .plain?: asking = step
        case nil: break
        }
    }

    private func confirm(_ step: RunbookPlanStep) {
        guard let index = run?.plan.steps.firstIndex(of: step) else { return }
        asking = nil
        typing = nil
        job.confirm(index)
    }

    private func decline() {
        if let step = asking, run?.progress.waiting?.id == step.id { declined = run?.progress.waiting?.index }
        asking = nil
    }
}

private struct RunbookStepStateRow: View {
    let step: RunbookPlanStep
    let state: RunbookStepState?
    let confirm: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Image(systemName: symbol).foregroundStyle(color).accessibilityHidden(true)
                Text(verbatim: step.id).font(.headline)
                Spacer()
                Text(label).font(.caption).foregroundStyle(color)
            }
            Text(verbatim: "\(step.action) · \(step.target.label)")
                .font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
            if let message = state?.message, !message.isEmpty {
                Text(verbatim: message).font(.caption).foregroundStyle(state?.state == .failed ? .red : .secondary)
            }
            if state?.state == .waiting {
                Button("Confirm this step…", action: confirm)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private var phase: RunbookStepPhase { state?.state ?? .pending }

    private var label: LocalizedStringKey {
        switch phase {
        case .waiting: "Waiting for you"
        case .running: "Running"
        case .ok: "Done"
        case .failed: "Failed"
        case .skipped: "Skipped"
        case .pending: "Not started"
        }
    }

    private var symbol: String {
        switch phase {
        case .waiting: "hand.raised"
        case .running: "arrow.triangle.2.circlepath"
        case .ok: "checkmark.circle.fill"
        case .failed: "xmark.octagon.fill"
        case .skipped: "minus.circle"
        case .pending: "circle"
        }
    }

    private var color: Color {
        switch phase {
        case .waiting: .orange
        case .running: .blue
        case .ok: .green
        case .failed: .red
        case .skipped, .pending: .secondary
        }
    }
}

private extension Array {
    subscript(safe index: Int) -> Element? { indices.contains(index) ? self[index] : nil }
}
