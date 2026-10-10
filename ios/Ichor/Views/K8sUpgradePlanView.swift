import SwiftUI
import IchorCore

/// Upgrade Kubernetes, like `talosctl upgrade-k8s`: a version the cluster's Talos supports, each
/// node's component images and the deprecated APIs still in use, a dry run, then the upgrade
/// (K8sUpgradeRunView), which goes on when leaving the screen.
struct K8sUpgradePlanView: View {
    @Environment(AppModel.self) private var model
    @State private var choice: LoadState<K8sVersionChoice> = .loading
    @State private var version = ""
    @State private var typed = ""
    @State private var plan: LoadState<K8sUpgradePlan>?
    @State private var dryRunFirst = true
    @State private var confirming = false
    @State private var message: String?

    private var job: K8sUpgradeJob { .shared }
    private var cluster: String { model.activeContext }

    var body: some View {
        Group {
            if job.version != nil {
                K8sUpgradeRunView(job: job, onUpgradeNow: {
                    job.clear()
                    confirming = true
                }, onClose: {
                    job.clear()
                    Task { await loadPlan() }
                })
            } else {
                planList
            }
        }
        .navigationTitle("Upgrade Kubernetes")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .sheet(isPresented: $confirming) {
            HostnameConfirmationSheet(
                title: String(localized: "Upgrade Kubernetes on \(cluster) to \(version)?"),
                message: String(localized: "The control planes restart their components one after the other, then each kubelet restarts. Going back to the previous version is another upgrade."),
                hostname: cluster,
                actionTitle: String(localized: "Upgrade now")
            ) {
                confirming = false
                Task { await start(dryRun: false) }
            }
        }
    }

    private var planList: some View {
        List {
            if model.activeSummary?.demo == true {
                Section { Text("Demo cluster: the plan is a sample, and the upgrade is refused.").note() }
            }
            Section {
                Text("Like talosctl upgrade-k8s: each control plane gets the new API server, controller manager, scheduler and kube-proxy images, one after the other, then every node gets the new kubelet. Each step is waited for.")
                    .font(.footnote).foregroundStyle(.secondary)
                switch choice {
                case .loading:
                    ProgressView().frame(maxWidth: .infinity)
                case .failed(let error):
                    Text(verbatim: error).foregroundStyle(.statusBad)
                    Button("Retry") { Task { await load() } }
                case .loaded(let c, _, _):
                    versionRows(c)
                }
            }
            if let plan {
                switch plan {
                case .loading:
                    Section { ProgressView().frame(maxWidth: .infinity) }
                case .failed(let error):
                    Section {
                        Text(verbatim: error).foregroundStyle(.statusBad)
                        Button("Retry") { Task { await loadPlan() } }
                    }
                case .loaded(let p, _, _):
                    planSections(p)
                }
            }
        }
        .themedBackground()
    }

    @ViewBuilder private func versionRows(_ c: K8sVersionChoice) -> some View {
        Text("Runs \(c.from.isEmpty ? "?" : c.from) · Talos supports \(c.supportedRange.isEmpty ? "?" : c.supportedRange)")
        if !c.versions.isEmpty {
            Picker("Version", selection: Binding(get: { version }, set: { pick($0) })) {
                ForEach(c.versions, id: \.self) { Text(verbatim: $0).tag($0) }
                if !version.isEmpty, !c.versions.contains(version) { Text(verbatim: version).tag(version) }
            }
        }
        if !c.warning.isEmpty { Text(verbatim: c.warning).font(.footnote).foregroundStyle(.secondary) }
        TextField(text: $typed, prompt: Text(verbatim: "1.X.Y")) { Text("Other version") }
            .keyboardType(.numbersAndPunctuation)
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
            .onSubmit { if c.allows(typed) { pick(typed) } }
        if !typed.isEmpty && !c.allows(typed) {
            Text("Not after the current version, more than one minor up, or outside what Talos supports.")
                .font(.footnote).foregroundStyle(.statusBad)
        }
    }

    @ViewBuilder private func planSections(_ p: K8sUpgradePlan) -> some View {
        stepSection("Control planes", p.controlPlaneSteps)
        stepSection("Kubelets", p.kubeletSteps)
        if !p.deprecatedAPIs.isEmpty {
            Section("Deprecated APIs still in use") {
                ForEach(p.deprecatedAPIs, id: \.api) { d in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack {
                            StatusPill(label: d.severity.isEmpty ? "info" : d.severity, color: d.severity == "critical" ? .statusBad : .statusWarn)
                            Text(verbatim: d.api).font(.caption.monospaced())
                        }
                        if !d.removedIn.isEmpty { Text("Removed in \(d.removedIn)").font(.caption).foregroundStyle(.secondary) }
                    }
                }
            }
        }
        Section("Checks") {
            ForEach(p.blockers, id: \.self) { blocker in
                Label { Text(verbatim: blocker) } icon: { Image(systemName: "xmark.octagon.fill") }.foregroundStyle(.statusBad)
            }
            ForEach(p.warnings, id: \.self) { warning in
                Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }.foregroundStyle(.statusWarn)
            }
            if p.blockers.isEmpty && p.warnings.isEmpty { Text("No issues found").foregroundStyle(.statusOK) }
        }
        Section {
            Toggle("Dry run first", isOn: $dryRunFirst)
            Button {
                if dryRunFirst { Task { await start(dryRun: true) } } else { confirming = true }
            } label: {
                Text(dryRunFirst ? "Run the dry run" : "Upgrade now").fontWeight(.semibold).frame(maxWidth: .infinity)
            }
            .disabled(!p.canStart)
            if let message { Text(verbatim: message).font(.footnote).foregroundStyle(.statusBad) }
        } footer: {
            Text("Each node checks its change without applying it; then the upgrade is offered.")
        }
    }

    @ViewBuilder private func stepSection(_ title: LocalizedStringKey, _ steps: [K8sPlanStep]) -> some View {
        if !steps.isEmpty {
            Section(title) {
                ForEach(steps) { s in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: "\(s.name) · \(s.component)")
                        Text(verbatim: "\(s.currentTag.isEmpty ? "?" : s.currentTag) → \(s.newTag)")
                            .font(.caption.monospaced())
                            .foregroundStyle(s.changed ? Color.primary : Color.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        }
    }

    private func pick(_ v: String) {
        version = v.trimmingCharacters(in: .whitespaces)
        Task { await loadPlan() }
    }

    private func load() async {
        guard let client = model.client else { return }
        choice = await .from { try await client.k8sUpgradeVersions() }
        if case .loaded(let c, _, _) = choice, version.isEmpty, let last = c.versions.last {
            version = last
            await loadPlan()
        }
    }

    private func loadPlan() async {
        guard let client = model.client, !version.isEmpty else { return }
        plan = .loading
        let picked = version
        let loaded = await LoadState.from { try await client.k8sUpgradePlan(version: picked) }
        if picked == version { plan = loaded }
    }

    /// A dry run changes nothing: it starts at once. The real run: a fresh Face ID / passcode
    /// first when the app lock is on.
    private func start(dryRun: Bool) async {
        message = nil
        if !dryRun, model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Upgrade Kubernetes on \(cluster)")) {
            message = failure
            return
        }
        guard let client = model.client else { return }
        job.start(client: client, version: version, dryRun: dryRun)
    }
}

/// The run (or dry run) as it goes, then how it ended; a dry run that passed offers the upgrade.
struct K8sUpgradeRunView: View {
    let job: K8sUpgradeJob
    let onUpgradeNow: () -> Void
    let onClose: () -> Void

    @State private var confirmCancel = false

    var body: some View {
        List {
            Section {
                if let outcome = job.outcome {
                    if let error = outcome {
                        Label { Text(verbatim: error) } icon: { Image(systemName: "xmark.octagon.fill") }.foregroundStyle(.statusBad)
                    } else if job.dryRun {
                        Label("Every node accepted its change. Nothing was applied.", systemImage: "checkmark.seal")
                            .foregroundStyle(.statusOK)
                    } else {
                        Label("Every component runs \(job.version ?? "").", systemImage: "checkmark.seal.fill").foregroundStyle(.statusOK)
                    }
                } else if let p = job.latest {
                    Text(verbatim: stepText(p)).font(.headline)
                    if !p.message.isEmpty { Text(verbatim: p.message).font(.footnote).foregroundStyle(.secondary) }
                } else {
                    Label("Starting…", systemImage: "hourglass")
                }
            } header: {
                Text(job.dryRun ? "Dry run of Kubernetes \(job.version ?? "")" : "Upgrading Kubernetes to \(job.version ?? "")")
            }
            if !job.events.isEmpty {
                Section("Steps") {
                    ForEach(Array(job.events.enumerated()), id: \.offset) { i, e in
                        HStack(spacing: 12) {
                            if i == job.events.count - 1 && job.isActive {
                                ProgressView()
                            } else {
                                Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
                            }
                            Text(verbatim: stepText(e))
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }
            Section {
                if job.isActive {
                    if job.cancelling {
                        Text("Stopping after the current node…").foregroundStyle(.secondary)
                    } else {
                        Button("Cancel", role: .destructive) { confirmCancel = true }
                    }
                } else {
                    if job.dryRun, case .some(.none) = job.outcome {
                        Button("Upgrade now", action: onUpgradeNow)
                    }
                    Button("Close", action: onClose)
                }
            }
        }
        .themedBackground()
        .confirmationDialog("Cancel the Kubernetes upgrade?", isPresented: $confirmCancel, titleVisibility: .visible) {
            Button("Stop after this node", role: .destructive) { job.cancel() }
            Button("Keep going", role: .cancel) {}
        } message: {
            Text("It stops after the node being changed, which is finished and waited for. The nodes done keep the new version; start again later to continue.")
        }
    }

    private func stepText(_ p: K8sUpgradeProgress) -> String {
        switch p.phase {
        case "done": String(localized: "Done")
        case "proxy": String(localized: "kube-proxy DaemonSet")
        default:
            p.total > 0
                ? String(localized: "Kubernetes upgrade \(min(p.index + 1, p.total))/\(p.total): \(p.name) \(p.component.isEmpty ? p.phase : p.component)")
                : p.message
        }
    }
}
