import SwiftUI
import IchorCore

/// A workload's actions beyond the restart (os:admin): scale a Deployment or StatefulSet like
/// `kubectl scale`, and roll a Deployment back to a revision like `kubectl rollout undo`,
/// then follow that rollout. `changed` refreshes the list behind.
struct WorkloadActionsSheet: View {
    let workload: KubeWorkload
    let changed: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var replicas: Int
    /// The replicas set now: the workload's, then the last scale's.
    @State private var current: Int
    @State private var scaling = false
    @State private var confirmScale = false
    @State private var confirmScaleToZero = false
    @State private var revisions: LoadState<[DeploymentRevision]> = .loading
    @State private var confirmRollback: DeploymentRevision?
    @State private var rollingBack = false
    /// Rolled back: its rollout is shown live.
    @State private var following: KubeWorkload?
    @State private var resultMessage: String?
    /// Argo CD self-heal would put the count back: asked first, offering to freeze the app.
    @State private var askArgo = false
    @State private var freezeFirst = false

    init(workload: KubeWorkload, changed: @escaping () async -> Void) {
        self.workload = workload
        self.changed = changed
        _replicas = State(initialValue: workload.desired)
        _current = State(initialValue: workload.desired)
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(verbatim: "\(workload.kind) · \(workload.namespace)")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                    Text("\(workload.ready)/\(workload.desired) ready").monospacedDigit()
                }
                if workload.canScale { scaleSection }
                if workload.hasHistory { historySection }
            }
            .themedBackground()
            .navigationTitle(Text(verbatim: workload.name))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .task { if workload.hasHistory { await loadRevisions() } }
            .confirmationDialog(String(localized: "Argo CD will revert this"), isPresented: $askArgo, titleVisibility: .visible,
                                presenting: argoOwner) { owner in
                Button(String(localized: "Freeze \(owner.app.name) 1 h, then scale")) { freezeFirst = true; confirmAfterArgo() }
                Button(String(localized: "Scale anyway")) { freezeFirst = false; confirmAfterArgo() }
                Button("Cancel", role: .cancel) {}
            } message: { owner in
                Text("\(owner.app.name) deploys this workload with self-heal on: within minutes it puts the replicas back as Git has them. Freeze \(owner.app.name) first to keep your change for a while.")
            }
            .confirmationDialog(String(localized: "Scale \(workload.name) to \(replicas)?"), isPresented: $confirmScale,
                                titleVisibility: .visible) {
                Button("Scale") { Task { await scale() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("From \(current) to \(replicas) replicas in \(workload.namespace), like kubectl scale.")
            }
            .sheet(isPresented: $confirmScaleToZero) {
                HostnameConfirmationSheet(
                    title: String(localized: "Scale \(workload.name) to 0?"),
                    message: String(localized: "Every pod of \(workload.name) stops: what it serves is down until it is scaled up again."),
                    hostname: workload.name,
                    actionTitle: String(localized: "Scale to 0")
                ) {
                    confirmScaleToZero = false
                    Task { await scale() }
                }
            }
            .confirmationDialog(confirmRollback.map { String(localized: "Roll back to revision \($0.revision)?") } ?? "",
                                isPresented: $confirmRollback.isPresent(),
                                titleVisibility: .visible,
                                presenting: confirmRollback) { revision in
                Button("Roll back", role: .destructive) { Task { await rollback(to: revision) } }
                Button("Cancel", role: .cancel) {}
            } message: { revision in
                Text("\(workload.name) gets the pod template of revision \(revision.revision) back: its pods are replaced with a rolling update, like kubectl rollout undo.")
            }
            .sheet(item: $following) { workload in
                RolloutStatusSheet(workload: workload) {
                    await changed()
                    await loadRevisions()
                }
            }
            .messageAlert($resultMessage)
        }
    }

    private var scaleSection: some View {
        Section {
            Stepper(value: $replicas, in: 0...kubeMaxScaleReplicas) {
                LabeledContent("Replicas") {
                    Text(replicas == current ? "\(replicas)" : "\(current) → \(replicas)").monospacedDigit()
                }
            }
            HStack {
                Button("Apply") {
                    freezeFirst = false
                    if argoOwner != nil { askArgo = true } else { confirmScaleNow() }
                }
                .disabled(replicas == current || scaling)
                if scaling {
                    Spacer()
                    ProgressView()
                }
            }
        } header: {
            Text("Scale")
        } footer: {
            Text("A HorizontalPodAutoscaler that manages it sets its own count again.")
        }
    }

    private var historySection: some View {
        Section("History") {
            switch revisions {
            case .loading:
                ProgressView()
            case .failed(let message):
                Text("Could not read its revisions: \(message)").note()
            case .loaded(let list, _, _):
                if list.isEmpty {
                    Text("No revision kept.").note()
                }
                ForEach(list) { revision in
                    RevisionRow(revision: revision, busy: rollingBack) { confirmRollback = revision }
                }
            }
        }
    }

    private func loadRevisions() async {
        guard let client = model.client else { return }
        revisions = revisions.refreshed(with: await .from { try await client.deploymentRevisions(workload) })
    }

    /// The Argo CD app that would revert a scale (self-heal on, not frozen) with its project, from
    /// the Argo CD status already loaded; nil when none or not loaded.
    private var argoOwner: ArgoOwnerOfWorkload? {
        guard let status = ArgoCDStore.shared.status(for: model.argoKey),
              let app = status.selfHealingOwner(kind: workload.kind, namespace: workload.namespace, name: workload.name),
              let project = status.project(of: app) else { return nil }
        return ArgoOwnerOfWorkload(app: app, project: project)
    }

    private func confirmScaleNow() {
        if scaleNeedsTypedName(replicas) { confirmScaleToZero = true } else { confirmScale = true }
    }

    /// The next confirmation, once the Argo CD dialog is gone (two dialogs at once would clash).
    private func confirmAfterArgo() {
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(350))
            confirmScaleNow()
        }
    }

    private func scale() async {
        guard let client = model.client, !scaling else { return }
        scaling = true
        defer { scaling = false }
        let target = replicas
        if freezeFirst, let owner = argoOwner {
            // Stored on the cluster: no name, which may be masked on screen.
            let options = freezeOptions(for: owner.app, scope: .app, minutes: freezeExtendMinutes, manualSync: true,
                                        reason: String(localized: "scale to \(target)"))
            if let failure = await ArgoCDStore.shared.freeze(.freeze, on: owner.project, options: [options], with: client) {
                resultMessage = String(localized: "Could not freeze \(owner.app.name), nothing scaled: \(failure)")
                return
            }
        }
        do {
            let warning = try await client.scale(workload, replicas: target)
            current = target
            let done = String(localized: "\(workload.name) is scaled to \(target)")
            resultMessage = warning.isEmpty ? done : done + "\n\n" + warning
            await changed()
        } catch {
            resultMessage = String(localized: "Could not scale \(workload.name): \(error.localizedDescription)")
        }
    }

    private func rollback(to revision: DeploymentRevision) async {
        guard let client = model.client, !rollingBack else { return }
        rollingBack = true
        defer { rollingBack = false }
        do {
            try await client.rollback(workload, to: revision.revision)
            following = workload
            await changed()
        } catch {
            resultMessage = String(localized: "Could not roll back \(workload.name): \(error.localizedDescription)")
        }
    }
}

/// The Argo CD app deploying a workload with self-heal, and its project.
private struct ArgoOwnerOfWorkload {
    let app: ArgoApp
    let project: ArgoProject
}

/// "Revision 3 · current", its age, images and change cause, and Roll back for the others.
private struct RevisionRow: View {
    let revision: DeploymentRevision
    let busy: Bool
    let onRollback: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text("Revision \(revision.revision)").font(.callout.weight(.semibold))
                    if revision.current { StatusPill(label: String(localized: "current"), color: .statusOK) }
                }
                if revision.created > 0 {
                    Text(Date(epochMillis: revision.created).formatted(.relative(presentation: .named)))
                        .font(.caption).foregroundStyle(.secondary)
                }
                ForEach(revision.images, id: \.self) { image in
                    Text(verbatim: image).font(.caption.monospaced()).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.middle)
                }
                if !revision.changeCause.isEmpty {
                    Text(verbatim: revision.changeCause).font(.caption)
                }
            }
            Spacer(minLength: 8)
            if !revision.current {
                Button("Roll back", action: onRollback)
                    .buttonStyle(.bordered)
                    .disabled(busy)
            }
        }
    }
}
