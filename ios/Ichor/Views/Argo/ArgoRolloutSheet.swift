import SwiftUI
import IchorCore

/// One rollout: its phase and steps, replicas, stable and new versions, analysis and images, then
/// the actions its state allows, each gated by what the credentials may do and confirmed first.
struct ArgoRolloutSheet: View {
    let rollout: ArgoRollout
    let busy: Bool
    /// The confirmed action.
    let onAction: (ArgoRolloutAction) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var confirming: ArgoRolloutAction?

    private var actions: [ArgoRolloutAction] { ArgoRolloutAction.allCases.filter(rollout.allows) }

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                details
                if !rollout.steps.isEmpty {
                    Section("Steps") {
                        ForEach(Array(rollout.steps.enumerated()), id: \.offset) { i, step in
                            stepRow(i, step)
                        }
                    }
                }
                if let analysis = rollout.analysis {
                    Section("Analysis") {
                        LabeledContent {
                            Text(verbatim: analysis.status)
                                .foregroundStyle(analysis.failed ? Color.statusBad : .secondary)
                        } label: {
                            Text(verbatim: analysis.name).font(.callout.monospaced()).lineLimit(2)
                        }
                        if !analysis.message.isEmpty {
                            Text(verbatim: analysis.message).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                if !rollout.images.isEmpty {
                    Section("Images") {
                        ForEach(rollout.images, id: \.self) { image in
                            Text(verbatim: image).font(.caption.monospaced()).lineLimit(2)
                        }
                    }
                }
                if !actions.isEmpty {
                    Section("Actions") {
                        ForEach(actions) { action in
                            Button(action.title, role: action == .abort ? .destructive : nil) { confirming = action }
                                .disabled(busy)
                                .kubeGated(.argoRollout, in: rollout.namespace)
                        }
                        KubeDeniedNote(.argoRollout, in: rollout.namespace)
                        if busy { Text("Action running…").font(.caption).foregroundStyle(.secondary) }
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .confirmationDialog(
                confirming.map { $0.question(rollout.name) } ?? "",
                isPresented: $confirming.isPresent(),
                titleVisibility: .visible, presenting: confirming
            ) { action in
                Button(action.title, role: action == .abort ? .destructive : nil) { onAction(action) }
                Button("Cancel", role: .cancel) {}
            } message: { action in
                Text(action.explanation)
            }
        }
        .loadsKubeActionAccess(namespace: rollout.namespace)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Image(systemName: rollout.glyph).foregroundStyle(rollout.level.color).accessibilityHidden(true)
                Text(verbatim: rollout.name).font(.headline.monospaced())
                Spacer()
                Tag(rollout.strategyLabel)
            }
            Text(verbatim: rollout.namespace).font(.caption).foregroundStyle(.secondary)
            if rollout.aborted {
                Tag(String(localized: "Aborted"), color: .statusBad)
            }
            if let paused = rollout.pausedLine {
                Text(verbatim: paused).font(.callout).foregroundStyle(.statusWarn)
            }
            ArgoRolloutStepsLine(rollout: rollout)
        }
    }

    @ViewBuilder private var details: some View {
        Section {
            if !rollout.phase.isEmpty {
                LabeledContent("Phase") { Text(verbatim: rollout.phase).foregroundStyle(rollout.level.color) }
            }
            if !rollout.message.isEmpty {
                LabeledContent("Message") {
                    Text(verbatim: rollout.message).font(.callout).multilineTextAlignment(.trailing)
                }
            }
            Text("\(String(rollout.ready)) ready of \(String(rollout.replicas)) · \(String(rollout.updated)) updated")
            if !rollout.stableHash.isEmpty || !rollout.currentHash.isEmpty {
                let stable = rollout.stableHash.isEmpty ? "-" : ArgoRollout.shortHash(rollout.stableHash)
                let current = rollout.currentHash.isEmpty ? "-" : ArgoRollout.shortHash(rollout.currentHash)
                Text("Stable \(stable) · new \(current)")
                    .font(.callout.monospaced())
            }
            if rollout.awaitsPromotion {
                Text("Waits for a promotion").foregroundStyle(.statusWarn)
            }
            if !rollout.workloadRef.isEmpty {
                Text("Pods from \(rollout.workloadRef)").font(.callout)
            }
        }
    }

    /// "3. setWeight 60 %", ticked when done, the current one in the rollout's colour.
    private func stepRow(_ i: Int, _ step: ArgoRolloutStep) -> some View {
        let done = i < rollout.step
        let current = i == rollout.step
        return HStack(spacing: 8) {
            Image(systemName: done ? "checkmark.circle.fill" : current ? "circle.inset.filled" : "circle")
                .foregroundStyle(done ? Color.green : current ? Color.orange : Color.secondary)
                .accessibilityHidden(true)
            // Kinds and labels are the CRD's words: never translated.
            Text(verbatim: "\(i + 1). \(step.kind)").font(.callout.monospaced())
            Spacer()
            if !step.label.isEmpty {
                Text(verbatim: step.label).font(.callout.monospaced()).foregroundStyle(.secondary)
            }
        }
    }
}
