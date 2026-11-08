import SwiftUI
import IchorCore

/// A workload's actions beyond the restart (os:admin): scale a Deployment or StatefulSet like
/// `kubectl scale`, pause or resume a Deployment's rollout, and roll a Deployment, StatefulSet
/// or DaemonSet back to a revision like `kubectl rollout undo` after seeing its pod template
/// diff, then follow that rollout. `changed` refreshes the list behind.
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
    @State private var revisions: LoadState<WorkloadRevisionList> = .loading
    /// The revision whose diff and rollback are shown.
    @State private var diffRevision: DeploymentRevision?
    @State private var rollingBack = false
    @State private var pausing = false
    /// The Deployment's rollout is paused: the workload's, then the last pause or resume's.
    @State private var rolloutPaused: Bool
    /// Rolled back: its rollout is shown live.
    @State private var following: KubeWorkload?
    @State private var resultMessage: String?
    /// Argo CD self-heal would put the count back: asked first, offering to freeze the app.
    @State private var askArgo = false
    @State private var freezeFirst = false
    /// kube-green has this namespace asleep (or sleeps it soon): said in the sheet, and warned
    /// again before a scale it would undo.
    @State private var kubeGreen: KubeGreenNamespace?

    init(workload: KubeWorkload, changed: @escaping () async -> Void) {
        self.workload = workload
        self.changed = changed
        _replicas = State(initialValue: workload.desired)
        _current = State(initialValue: workload.desired)
        _rolloutPaused = State(initialValue: workload.rolloutPaused)
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(verbatim: "\(workload.kind) · \(workload.namespace)")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                    Text("\(workload.ready)/\(workload.desired) ready").monospacedDigit()
                    if let kubeGreen {
                        KubeGreenAsleepLabel(status: kubeGreen, kind: workload.kind)
                    }
                    if let selection = workload.podSelection {
                        NavigationLink {
                            WorkloadPodsView(workload: workload, selection: selection)
                        } label: {
                            Label("Pods", systemImage: "cube")
                        }
                    }
                    NavigationLink {
                        WorkloadLogsSheet(workload: workload)
                    } label: {
                        Label("Logs of all pods", systemImage: "doc.text")
                    }
                }
                if workload.canPauseRollout { pauseSection }
                if workload.canScale { scaleSection }
                if workload.hasHistory { historySection }
                Section {
                    KubeEventsRows(namespace: workload.namespace, kind: "", name: workload.name)
                } header: {
                    Text(verbatim: CheckupText.kubeEventsTitle)
                }
            }
            .loadsKubeActionAccess(namespace: workload.namespace)
            .themedBackground()
            .navigationTitle(Text(verbatim: workload.name))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                ToolbarItemGroup(placement: .topBarLeading) {
                    ShareLinkButton(
                        target: .workload(kind: workload.kind, namespace: workload.namespace, name: workload.name))
                    PinButton(
                        target: .workload(kind: workload.kind, namespace: workload.namespace, name: workload.name),
                        label: workload.name)
                }
            }
            .task {
                if workload.hasHistory { await loadRevisions() }
                await loadKubeGreen()
            }
            .confirmationDialog(
                String(localized: "Argo CD will revert this"), isPresented: $askArgo, titleVisibility: .visible,
                presenting: argoOwner
            ) { owner in
                Button(String(localized: "Freeze \(owner.app.name) 1 h, then scale")) {
                    freezeFirst = true; confirmAfterArgo()
                }
                Button(String(localized: "Scale anyway")) {
                    freezeFirst = false; confirmAfterArgo()
                }
                Button("Cancel", role: .cancel) {}
            } message: { owner in
                Text(
                    "\(owner.app.name) deploys this workload with self-heal on: within minutes it puts the replicas back as Git has them. Freeze \(owner.app.name) first to keep your change for a while."
                )
            }
            .confirmationDialog(
                String(localized: "Scale \(workload.name) to \(replicas)?"), isPresented: $confirmScale,
                titleVisibility: .visible
            ) {
                Button("Scale") { Task { await scale() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(
                    verbatim: [
                        String(
                            localized:
                                "From \(current) to \(replicas) replicas in \(workload.namespace), like kubectl scale."),
                        kubeGreenWarning,
                    ]
                    .compactMap { $0 }
                    .joined(separator: "\n\n"))
            }
            .sheet(isPresented: $confirmScaleToZero) {
                HostnameConfirmationSheet(
                    title: String(localized: "Scale \(workload.name) to 0?"),
                    message: [
                        String(
                            localized:
                                "Every pod of \(workload.name) stops: what it serves is down until it is scaled up again."
                        ), kubeGreenWarning,
                    ]
                    .compactMap { $0 }
                    .joined(separator: "\n\n"),
                    hostname: workload.name,
                    actionTitle: String(localized: "Scale to 0")
                ) {
                    confirmScaleToZero = false
                    Task { await scale() }
                }
            }
            .sheet(item: $diffRevision) { revision in
                RevisionDiffSheet(
                    workload: workload, revision: revision, history: loadedHistory ?? WorkloadRevisionList()
                ) {
                    // The diff sheet goes first: the rollout sheet follows once it is gone.
                    diffRevision = nil
                    try? await Task.sleep(for: .milliseconds(350))
                    await rollback(to: revision)
                }
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

    /// Pause a Deployment's rollout (a template change then waits), or resume a paused one.
    private var pauseSection: some View {
        Section {
            HStack {
                Button {
                    Task { await pauseRollout(!rolloutPaused) }
                } label: {
                    if rolloutPaused { Text("Resume rollout") } else { Text("Pause rollout") }
                }
                .disabled(pausing)
                .kubeGated(.pauseRollout)
                if pausing {
                    Spacer()
                    ProgressView()
                }
            }
            KubeDeniedNote(.pauseRollout)
        } footer: {
            if rolloutPaused {
                Text("Rollout paused: template changes wait until it resumes")
            }
        }
    }

    private var scaleSection: some View {
        Section {
            if let autoscaler = workload.autoscaler {
                NavigationLink {
                    AutoscalingView()
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "arrow.up.and.down")
                            .foregroundStyle(autoscaler.stateOk ? Color.secondary : Color.statusWarn)
                        Text(verbatim: autoscaler.label)
                            .font(.callout.monospaced())
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel(Text("Autoscaled by") + Text(verbatim: " " + autoscaler.label))
                }
            }
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
                .kubeGated(KubeAction.scale(kind: workload.kind))
                if scaling {
                    Spacer()
                    ProgressView()
                }
            }
            KubeDeniedNote(KubeAction.scale(kind: workload.kind))
        } header: {
            Text("Scale")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if replicas != current, let kubeGreenWarning {
                    Text(verbatim: kubeGreenWarning).foregroundStyle(Color.statusWarn)
                }
                if workload.autoscaler == nil {
                    Text("A HorizontalPodAutoscaler that manages it sets its own count again.")
                }
            }
        }
    }

    private var historySection: some View {
        Section("History") {
            switch revisions {
            case .loading:
                ProgressView()
            case .failed(let message):
                Text("Could not read its revisions: \(message)").note()
            case .loaded(let history, _, _):
                let list = history.revisions
                if list.isEmpty {
                    Text("No revision kept.").note()
                }
                KubeDeniedNote(KubeAction.rollback(kind: workload.kind))
                ForEach(list) { revision in
                    RevisionRow(revision: revision, kind: workload.kind, busy: rollingBack) { diffRevision = revision }
                }
            }
        }
    }

    /// The revisions once read, nil while loading or after a failure.
    private var loadedHistory: WorkloadRevisionList? {
        if case .loaded(let history, _, _) = revisions { history } else { nil }
    }

    private func loadRevisions() async {
        guard let client = model.client else { return }
        revisions = revisions.refreshed(with: await .from { try await client.workloadRevisions(workload) })
    }

    private func loadKubeGreen() async {
        guard let client = model.client else { return }
        kubeGreen = await KubeGreenStore.shared.load(
            namespace: workload.namespace, key: model.kubeGreenKey, with: client)
    }

    /// The warning to show before a scale kube-green would undo; nil when it will not touch it.
    private var kubeGreenWarning: String? { kubeGreenChangeWarning(kubeGreen) }

    /// The Argo CD app that would revert a scale (self-heal on, not frozen) with its project, from
    /// the Argo CD status already loaded; nil when none or not loaded.
    private var argoOwner: ArgoSelfHealer? {
        ArgoCDStore.shared.selfHealer(
            kind: workload.kind, namespace: workload.namespace, name: workload.name, key: model.argoKey)
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
        if freezeFirst, let owner = argoOwner,
            let failure = await ArgoCDStore.shared.freezeForHandChange(
                owner, reason: String(localized: "scale to \(target)"), with: client)
        {
            resultMessage = failure
            return
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
            if loadedHistory?.manual == true {
                resultMessage = String(
                    localized: "\(workload.name) rolled back: its pods update when deleted (OnDelete).")
            }
            following = workload
            await changed()
        } catch {
            resultMessage = String(localized: "Could not roll back \(workload.name): \(error.localizedDescription)")
        }
    }

    private func pauseRollout(_ paused: Bool) async {
        guard let client = model.client, !pausing else { return }
        pausing = true
        defer { pausing = false }
        do {
            try await client.pauseRollout(workload, paused: paused)
            rolloutPaused = paused
            resultMessage =
                paused
                ? String(localized: "Rollout of \(workload.name) paused")
                : String(localized: "Rollout of \(workload.name) resumed")
            if !paused { following = workload }
            await changed()
        } catch {
            resultMessage = String(
                localized: "Could not pause or resume the rollout of \(workload.name): \(error.localizedDescription)")
        }
    }
}

/// "Revision 3 · current", its age, images and change cause, and Roll back for the others,
/// which opens the revision's diff first.
private struct RevisionRow: View {
    let revision: DeploymentRevision
    let kind: String
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
                    Text(relativeTime(revision.created))
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
                    .kubeGated(KubeAction.rollback(kind: kind))
            }
        }
    }
}
