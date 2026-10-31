import SwiftUI
import IchorCore

/// A runbook's plan, from its row: each step with its target, whether it is destructive, the
/// role it needs and whether the cluster allows it. A refused step keeps Run off until it is
/// let through ("Continue on error"), unless its YAML already says so. Run starts the run and
/// opens its screen; each destructive step is confirmed there. Same as Android's RunbookPlanScreen.kt.
struct RunbookPlanView: View {
    let id: String
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    @State private var yaml: String?
    @State private var plan: RunbookPlan?
    @State private var params: [RunbookParam] = []
    @State private var values: [String: String] = [:]
    @State private var choices: [String: [String]] = [:]
    @State private var continueIDs: Set<String> = []
    @State private var failure: String?

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }
    private var job: RunbookJob { RunbookJob.shared }

    var body: some View {
        Form {
            if let failure, plan == nil, params.isEmpty {
                Section { LoadFailureRow(message: failure, retry: { await load() }) }
            }
            if plan == nil, !params.isEmpty {
                RunbookParamsForm(params: params, values: $values, choices: choices, failure: failure) {
                    Task { await planWithValues() }
                }
            } else if let plan {
                if !params.isEmpty {
                    Section {
                        Text("Values: \(RunbookParam.label(values))").font(.footnote).foregroundStyle(.secondary)
                        Button("Change the values") {
                            self.plan = nil
                            continueIDs = []
                        }
                    }
                }
                RunbookPlanSections(plan: plan, continueIDs: $continueIDs, editable: true)
                Section {
                    Button {
                        start(plan)
                    } label: {
                        Label("Run", systemImage: "play.fill")
                    }
                    .disabled(!plan.canRun(continueIDs: continueIDs) || job.isRunning || yaml == nil)
                    if job.isRunning {
                        Text("Another runbook is running: open it from its row or its Live Activity.")
                            .font(.footnote).foregroundStyle(.secondary)
                    } else if !plan.canRun(continueIDs: continueIDs) {
                        Text(
                            "A step the cluster refuses would stop the run: let it through with Continue on error to run the rest."
                        )
                        .font(.footnote).foregroundStyle(.secondary)
                    }
                }
            } else if failure == nil {
                ProgressView()
            }
        }
        .navigationTitle(plan.map { Text(verbatim: $0.name) } ?? Text("Runbook"))
        .refreshable { await load() }
        .task(id: id) { await load() }
    }

    private func load() async {
        do {
            let text = try await RunbookStore.shared.yaml(id, fingerprint: fingerprint)
            guard let client = model.client else {
                failure = String(localized: "the cluster cannot be reached now.")
                return
            }
            yaml = text
            params = try await TalosClient.runbookSummary(yaml: text).params
            if params.isEmpty {
                await planWithValues()
            } else if plan == nil {
                values = RunbookParam.initialValues(params, remembered: remembered)
                choices = await client.runbookValueChoices(params)
            } else {
                await planWithValues()
            }
        } catch {
            failure = error.localizedDescription
        }
    }

    /// The plan of the values given; what the core says of them stays on screen.
    private func planWithValues() async {
        guard let yaml, let client = model.client else { return }
        do {
            plan = try await client.runbookPlan(yaml: yaml, values: values)
            failure = nil
            if !params.isEmpty { UserDefaults.standard.set(values, forKey: memoryKey) }
        } catch {
            failure = error.localizedDescription
        }
    }

    /// The values last given to this runbook of this cluster: a rerun starts from them.
    private var memoryKey: String { "runbook.values.\(fingerprint).\(id)" }
    private var remembered: [String: String] {
        UserDefaults.standard.dictionary(forKey: memoryKey) as? [String: String] ?? [:]
    }

    private func start(_ plan: RunbookPlan) {
        guard let yaml, let client = model.client else { return }
        if job.start(
            id: id, fingerprint: fingerprint, yaml: yaml, plan: plan, continueIDs: continueIDs, values: values,
            client: client)
        {
            path.append(.runbookRun(id: id))
        }
    }
}

/// A plan's steps, in order, for the plan screen and the import's consent step.
struct RunbookPlanSections: View {
    let plan: RunbookPlan
    @Binding var continueIDs: Set<String>
    /// The refused steps get their "Continue on error" toggle.
    let editable: Bool

    var body: some View {
        if !plan.description.isEmpty {
            Section { Text(verbatim: plan.description).foregroundStyle(.secondary) }
        }
        if !plan.errors.isEmpty {
            Section {
                ForEach(plan.errors, id: \.self) { Text(verbatim: $0).foregroundStyle(.red) }
            }
        }
        Section {
            ForEach(Array(plan.steps.enumerated()), id: \.element.id) { index, step in
                RunbookPlanStepRow(index: index, step: step)
                if editable, !step.allowed, !step.continueOnError {
                    Toggle(
                        "Continue on error",
                        isOn: Binding(
                            get: { continueIDs.contains(step.id) },
                            set: { on in
                                if on { continueIDs.insert(step.id) } else { continueIDs.remove(step.id) }
                            }
                        ))
                }
            }
        } header: {
            Text("\(String(plan.steps.count)) steps")
        }
    }
}

private struct RunbookPlanStepRow: View {
    let index: Int
    let step: RunbookPlanStep

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: "\(index + 1). \(step.id)").font(.headline)
                Spacer()
                if step.destructive {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                        .accessibilityLabel(Text("Destructive"))
                }
            }
            Text(verbatim: "\(step.action) · \(step.target.label)")
                .font(.system(.subheadline, design: .monospaced))
            if !step.detail.isEmpty {
                Text(verbatim: step.detail).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: step.role).font(.caption).foregroundStyle(.secondary)
            if step.allowed {
                if step.continueOnError {
                    Text("Continues on error").font(.caption).foregroundStyle(.secondary)
                }
            } else {
                Text(verbatim: step.denied).font(.caption).foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
