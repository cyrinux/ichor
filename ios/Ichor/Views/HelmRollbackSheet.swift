import SwiftUI
import IchorCore

/// Rolling a Helm release back to an older revision: what would change, each change dry-run on
/// the API server first (nothing has changed yet), then the confirm button. A Flux-managed
/// release is said to be suspended first; a blocker (a missing revision, the demo cluster…)
/// keeps the button disabled. The options (wait, replace, keep the last N revisions) are
/// helm rollback's flags; with Wait on, the sheet then follows the release's workloads.
struct HelmRollbackSheet: View {
    let namespace: String
    let name: String
    let revision: Int
    /// After a successful rollback (the wait, if any, comes after), before the sheet dismisses.
    let rolledBack: () -> Void
    /// Reads the release again (after a failed rollback: something may have moved).
    let reload: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<HelmRollbackPlan> = .loading
    @State private var options = HelmRollbackOptions()
    /// The plan is read again after an option changed; the older one stays on screen.
    @State private var planning = false
    @State private var running = false
    @State private var failure: String?
    /// The rollback is done but the old revisions were not deleted: said once, then on as usual.
    @State private var pruneWarning: String?
    /// The rollback is done and the sheet follows its workloads.
    @State private var waiting = false
    @State private var follow: HelmRollbackFollowHandle?
    @State private var waitProgress: HelmRollbackFollowProgress?
    @State private var waitOutcome: FollowOutcome?

    var body: some View {
        NavigationStack {
            Group {
                if waiting {
                    List {
                        HelmRollbackWaitSection(progress: waitProgress, outcome: waitOutcome, leave: leave)
                    }
                    .themedBackground()
                } else {
                    planView
                }
            }
            .navigationTitle(Text("Roll back to revision \(String(revision))"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    if !waiting {
                        Button("Cancel") { dismiss() }.disabled(running)
                    }
                }
            }
        }
        .interactiveDismissDisabled(running)
        .alert("Warnings", isPresented: $pruneWarning.isPresent()) {
            Button("OK") { if !waiting { dismiss() } }
        } message: {
            Text(verbatim: pruneWarning ?? "")
        }
        .task(id: options.planKey) { await load() }
        .onDisappear { follow?.cancel() }
        .loadsKubeActionAccess(namespace: namespace)
    }

    @ViewBuilder private var planView: some View {
        switch state {
        case .loading:
            VStack(spacing: 12) {
                ProgressView()
                Text("Checking what would change…").note()
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed:
            LoadStateView(state: state, retry: load) { _ in EmptyView() }
        case .loaded(let plan, _, _):
            content(plan)
        }
    }

    private func content(_ plan: HelmRollbackPlan) -> some View {
        List {
            Section {
                LabeledContent("Now") { Text(verbatim: versionLine(plan.from, plan.fromChart, plan.fromAppVersion)).monospaced() }
                LabeledContent("After") { Text(verbatim: versionLine(plan.to, plan.toChart, plan.toAppVersion)).monospaced() }
            }
            if !plan.fluxOwner.isEmpty {
                Section {
                    Label {
                        Text("The Flux HelmRelease \(plan.fluxOwner) manages this release: it is suspended first, otherwise Flux would upgrade it straight back. Fix the version in Git, then resume it.")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    .font(.callout)
                    .foregroundStyle(Color.statusWarn)
                }
            }
            if !plan.blockers.isEmpty {
                Section {
                    ForEach(Array(plan.blockers.enumerated()), id: \.offset) { _, blocker in
                        Text(verbatim: blocker).font(.callout).foregroundStyle(Color.statusBad)
                    }
                } header: {
                    Text("Cannot roll back")
                }
            }
            HelmRollbackOptionsSection(plan: plan, options: $options, planning: planning, disabled: running)
            Section {
                if plan.changes.isEmpty {
                    Text("No object changes: only the release record moves back.").note()
                } else {
                    ForEach(plan.changes) { HelmRollbackChangeRow(change: $0) }
                }
            } header: {
                Text("Changes")
            } footer: {
                footer(plan)
            }
            Section {
                if let failure {
                    Text(verbatim: failure).font(.callout).foregroundStyle(Color.statusBad)
                }
                KubeDeniedNote(.helmRollback)
                confirmButton(plan)
            }
        }
        .themedBackground()
    }

    @ViewBuilder private func footer(_ plan: HelmRollbackPlan) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if plan.unchanged > 0 {
                Text("\(String(plan.unchanged)) objects already match and are left alone.")
            }
            if plan.keepsObjects {
                Text("Objects marked helm.sh/resource-policy: keep stay, as with helm rollback.")
            }
            if plan.canRun {
                Text("Each change passed a dry run on the API server. Nothing has changed yet.")
            }
            Text("Like helm rollback: a new revision is recorded with the older one's chart and values, and helm history agrees afterwards.")
        }
    }

    private func confirmButton(_ plan: HelmRollbackPlan) -> some View {
        Button(role: .destructive) {
            Task { await rollBack(plan) }
        } label: {
            Group {
                if running {
                    ProgressView()
                } else {
                    Text("Roll back to revision \(String(revision))").bold()
                }
            }
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(.red)
        .controlSize(.large)
        .disabled(!plan.canRun || running || planning)
        .kubeGated(.helmRollback)
        .listRowBackground(Color.clear)
        .listRowInsets(EdgeInsets())
    }

    /// "#3 · site-1.1.0 · app 1.1".
    private func versionLine(_ number: Int, _ chart: String, _ appVersion: String) -> String {
        let app = appVersion.isEmpty ? "" : String(localized: "app \(appVersion)")
        return ["#\(number)", chart, app].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// The plan for the current options; after an option changed the older plan stays on
    /// screen until the new one is read, and a read for older options is dropped.
    private func load() async {
        guard let client = model.client else { return }
        let asked = options
        if case .loaded = state { planning = true } else { state = .loading }
        let fetched: LoadState<HelmRollbackPlan> = await .from {
            try await client.helmRollbackPlan(namespace: namespace, name: name, revision: revision, options: asked)
        }
        guard !Task.isCancelled, asked.planKey == options.planKey else { return }
        planning = false
        state = fetched
    }

    private func rollBack(_ plan: HelmRollbackPlan) async {
        guard let client = model.client, !running else { return }
        running = true
        failure = nil
        do {
            try await client.helmRollback(namespace: namespace, name: name, revision: revision, options: options)
            running = false
            // The rollback is done: the release screen says so whenever the sheet goes.
            rolledBack()
            if options.wait && plan.canWait {
                await followWorkloads(client)
            } else {
                dismiss()
            }
        } catch where isHelmHistoryNotPruned(error.localizedDescription) {
            // Done, only the old revisions are still there: as a success, with the warning.
            running = false
            rolledBack()
            pruneWarning = error.localizedDescription
            if options.wait && plan.canWait {
                await followWorkloads(client)
            }
        } catch {
            failure = error.localizedDescription
            running = false
            await reload()
        }
    }

    /// helm rollback --wait: the release's workloads until they are ready, fail or 5 minutes pass.
    private func followWorkloads(_ client: TalosClient) async {
        waiting = true
        let handle = client.startHelmRollbackFollow(namespace: namespace, name: name)
        follow = handle
        for await event in handle.events {
            switch event {
            case .progress(let progress): waitProgress = progress
            case .done(let result, let error): waitOutcome = .helmWait(result: result, error: error)
            }
        }
    }

    /// Stop following (the rollback stays done), or Done once the wait ended.
    private func leave() {
        follow?.cancel()
        dismiss()
    }
}

/// An object of the rollback: what happens to it (create, update, delete, kept), which one,
/// and why the dry run refused it.
private struct HelmRollbackChangeRow: View {
    let change: HelmRollbackChange

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Tag(actionLabel, color: actionColor)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: change.label)
                    .font(.callout.monospaced())
                    .lineLimit(2)
                    .truncationMode(.middle)
                if !change.error.isEmpty {
                    Text(verbatim: change.error).font(.caption).foregroundStyle(Color.statusBad)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var actionLabel: String {
        switch change.action {
        case "create": String(localized: "create")
        case "update": String(localized: "update")
        case "replace": String(localized: "replace")
        case "delete": String(localized: "delete")
        case "keep": String(localized: "kept")
        default: change.action
        }
    }

    private var actionColor: Color? {
        switch change.action {
        case "create": Color.statusOK
        case "update", "replace": Color.statusWarn
        case "delete": Color.statusBad
        default: nil
        }
    }
}
