import SwiftUI
import IchorCore

/// `talosctl upgrade` of one node (os:admin): plan with blockers and warnings, target version
/// and image, then the progress until the node is back. One upgrade at a time in the app.
struct UpgradeView: View {
    let node: String
    let hostname: String

    /// `initialVersion` preselects the target (from the overview's update banner).
    init(node: String, hostname: String, initialVersion: String = "") {
        self.node = node
        self.hostname = hostname
        _version = State(initialValue: initialVersion)
    }

    @Environment(AppModel.self) private var model
    @State private var plan: LoadState<UpgradePlan> = .loading
    @State private var releases: [TalosRelease] = []
    @State private var releasesError: String?
    @State private var version = ""
    @State private var showPrereleases = false
    @State private var image = ""
    @State private var stage = false
    @State private var force = false
    @State private var confirmForce = false
    @State private var confirming = false
    @State private var message: String?

    private var job: UpgradeJob { .shared }

    var body: some View {
        Group {
            if let target = job.target, target.node == node {
                UpgradeProgressView(job: job, hostname: hostname) { Task { await loadPlan() } }
            } else {
                LoadStateView(state: plan, retry: loadPlan) { plan in form(plan) }
            }
        }
        .navigationTitle(String(localized: "Upgrade · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadPlan() }
        .task { await loadReleases() }
        // The node runs another Talos version now: what it can do is asked again.
        .onChange(of: job.outcome) { _, outcome in
            if case .succeeded? = outcome, let target = job.target {
                Task { await model.reloadFeatures(node: target.node) }
            }
        }
        .task(id: [version, loadedPlan?.currentImage ?? ""]) { await computeImage() }
    }

    private var loadedPlan: UpgradePlan? {
        if case .loaded(let plan, _) = plan { return plan }
        return nil
    }

    private func makeGate(_ plan: UpgradePlan) -> UpgradeGate {
        UpgradeGate(plan: plan, targetVersion: version, force: force, busy: job.isActive)
    }

    private func form(_ plan: UpgradePlan) -> some View {
        let gate = makeGate(plan)
        return Form {
            if job.isActive, let other = job.target {
                Section {
                    Text("An upgrade of \(other.hostname) is in progress. The app runs one upgrade at a time.")
                        .foregroundStyle(.orange)
                }
            }
            currentSection(plan)
            if let etcd = plan.etcd { etcdSection(etcd) }
            targetSection(plan, gate: gate)
            checksSection(plan, gate: gate)
            Section {
                Button(role: .destructive) { Task { await requestStart() } } label: {
                    Text(stage ? String(localized: "Stage upgrade") : String(localized: "Start upgrade"))
                        .fontWeight(force ? .bold : .regular)
                }
                .disabled(!gate.canStart || image.isEmpty)
                if let message { Text(message).font(.footnote).foregroundStyle(.red) }
            } footer: {
                Text("The node reboots into the new version. An upgrade cannot be cancelled once requested.")
            }
        }
        .themedBackground()
        .toolbar {
            if gate.forceAvailable {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button(role: .destructive) { confirmForce = true } label: {
                            Label("Force upgrade…", systemImage: "exclamationmark.triangle")
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                }
            }
        }
        .alert(Text("Skip the etcd checks?"), isPresented: $confirmForce) {
            Button("Force", role: .destructive) { force = true }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("--force upgrades even though etcd is unhealthy or would lose quorum. The cluster can become unavailable and etcd data can be lost.")
        }
        .sheet(isPresented: $confirming) {
            HostnameConfirmationSheet(
                title: String(localized: "Upgrade \(hostname)?"),
                message: confirmationMessage(plan),
                hostname: hostname,
                actionTitle: stage ? String(localized: "Stage upgrade") : String(localized: "Start upgrade")
            ) {
                confirming = false
                start(plan)
            }
        }
    }

    private func currentSection(_ plan: UpgradePlan) -> some View {
        Section("Current") {
            LabeledContent("Version", value: plan.currentVersion.isEmpty ? "—" : plan.currentVersion)
            LabeledContent("Role", value: plan.controlPlane ? String(localized: "Control plane") : String(localized: "Worker"))
            VStack(alignment: .leading, spacing: 2) {
                Text("Image").font(.caption).foregroundStyle(.secondary)
                Text(verbatim: plan.currentImage.isEmpty ? "—" : plan.currentImage)
                    .font(.caption.monospaced()).textSelection(.enabled)
            }
            if !plan.schematic.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Image Factory schematic").font(.caption).foregroundStyle(.secondary)
                    Text(verbatim: plan.schematic).font(.caption2.monospaced()).textSelection(.enabled)
                }
            }
        }
    }

    private func etcdSection(_ etcd: UpgradePlan.Etcd) -> some View {
        Section {
            LabeledContent("Healthy members", value: "\(etcd.healthy) / \(etcd.members)")
            if etcd.thisNodeMember {
                LabeledContent("Quorum while this node is down") {
                    Text(etcd.quorumAfterLoss ? String(localized: "Kept") : String(localized: "Lost"))
                        .foregroundStyle(etcd.quorumAfterLoss ? Color.green : Color.red)
                }
            }
        } header: {
            Text(verbatim: "etcd")
        }
    }

    private func targetSection(_ plan: UpgradePlan, gate: UpgradeGate) -> some View {
        Section {
            TextField("Version, e.g. v1.11.3", text: $version)
                .font(.body.monospaced())
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            if gate.invalidVersion && !version.trimmingCharacters(in: .whitespaces).isEmpty {
                Text("Not a Talos version (vMAJOR.MINOR.PATCH).").font(.footnote).foregroundStyle(.red)
            }
            let suggestions = upgradeSuggestions(releases, current: plan.currentVersion, includePrerelease: showPrereleases)
            ForEach(suggestions.prefix(8)) { release in
                Button { version = release.version } label: { ReleaseRow(release: release, selected: normalizedTalosVersion(version) == release.version) }
                    .buttonStyle(.plain)
            }
            if releases.contains(where: \.prerelease) {
                Toggle("Show pre-releases", isOn: $showPrereleases)
            }
            if let releasesError {
                Text("Releases unavailable (\(releasesError)): type the version.").font(.footnote).foregroundStyle(.secondary)
            }
            if !image.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("New image").font(.caption).foregroundStyle(.secondary)
                    Text(verbatim: image).font(.caption.monospaced()).textSelection(.enabled)
                }
            }
            Toggle(isOn: $stage) {
                VStack(alignment: .leading) {
                    Text("Stage (--stage)")
                    Text("Installs during the reboot instead of before it, for nodes whose files in use block the upgrade.")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Target")
        }
    }

    @ViewBuilder private func checksSection(_ plan: UpgradePlan, gate: UpgradeGate) -> some View {
        if !plan.blockers.isEmpty || !plan.warnings.isEmpty || gate.downgrade || gate.sameVersion || force {
            Section("Checks") {
                ForEach(plan.blockers, id: \.self) { blocker in
                    Label { Text(verbatim: blocker) } icon: { Image(systemName: "xmark.octagon.fill") }
                        .foregroundStyle(.red)
                }
                ForEach(plan.warnings, id: \.self) { warning in
                    Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                        .foregroundStyle(.orange)
                }
                if gate.downgrade {
                    Label("This is a downgrade: not every Talos version supports going back.", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                }
                if gate.sameVersion {
                    Label("This node already runs this version: the image is reinstalled.", systemImage: "info.circle")
                        .foregroundStyle(.secondary)
                }
                if force {
                    HStack {
                        Label("Force: etcd checks skipped", systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.red).fontWeight(.semibold)
                        Spacer()
                        Button("Turn off") { force = false }.buttonStyle(.borderless)
                    }
                }
            }
        }
    }

    private func confirmationMessage(_ plan: UpgradePlan) -> String {
        let target = normalizedTalosVersion(version) ?? version
        var text = String(localized: "\(hostname) installs \(target) and reboots: its pods are drained and it is unavailable for several minutes.")
        if plan.controlPlane { text += "\n\n" + String(localized: "Control-plane node: it leaves etcd while down. Make sure the other members are healthy, or the cluster can lose quorum.") }
        if force { text += "\n\n" + String(localized: "Force is on: the etcd checks are skipped.") }
        return text
    }

    private func loadPlan() async {
        guard let client = model.client else { return }
        plan = await .from { try await client.upgradePlan(node: node) }
        force = false
        suggestDefaultVersion()
    }

    private func loadReleases() async {
        do {
            releases = try await TalosClient.talosReleases()
            releasesError = nil
            suggestDefaultVersion()
        } catch {
            releasesError = error.localizedDescription
        }
    }

    /// The newest stable release above the running one, once both are known.
    private func suggestDefaultVersion() {
        guard version.isEmpty, let plan = loadedPlan,
              let latest = upgradeSuggestions(releases, current: plan.currentVersion, includePrerelease: false).first,
              !isTalosDowngrade(from: plan.currentVersion, to: latest.version) else { return }
        version = latest.version
    }

    private func computeImage() async {
        guard let plan = loadedPlan, let target = normalizedTalosVersion(version) else {
            image = ""
            return
        }
        image = await TalosClient.upgradeImage(currentImage: plan.currentImage, version: target)
    }

    /// App lock first (like reboot), then the typed hostname.
    private func requestStart() async {
        message = nil
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Upgrade \(hostname)")) {
            message = failure
            return
        }
        confirming = true
    }

    private func start(_ plan: UpgradePlan) {
        guard let client = model.client, let target = normalizedTalosVersion(version), makeGate(plan).canStart, !image.isEmpty else { return }
        job.start(client: client, target: UpgradeJob.Target(node: node, hostname: hostname, fromVersion: plan.currentVersion,
                                                           toVersion: target, image: image, stage: stage),
                  force: force && makeGate(plan).forceAvailable)
    }
}

private struct ReleaseRow: View {
    let release: TalosRelease
    let selected: Bool

    var body: some View {
        HStack {
            Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                .foregroundStyle(selected ? Color.accentColor : Color.secondary)
            Text(verbatim: release.version).font(.body.monospaced())
            if release.prerelease { StatusPill(label: String(localized: "pre-release"), color: .orange) }
            Spacer()
            if !release.date.isEmpty { Text(verbatim: String(release.date.prefix(10))).font(.caption).foregroundStyle(.secondary) }
        }
        .contentShape(Rectangle())
    }
}

/// Phase timeline of the followed upgrade, then its result.
private struct UpgradeProgressView: View {
    let job: UpgradeJob
    let hostname: String
    let onClose: () -> Void

    var body: some View {
        List {
            if let target = job.target {
                Section {
                    LabeledContent("Version", value: "\(target.fromVersion.isEmpty ? "—" : target.fromVersion) → \(target.toVersion)")
                    Text(verbatim: target.image).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
            Section("Progress") {
                ForEach(job.timeline) { step in StepRow(step: step) }
            }
            resultSection
        }
        .themedBackground()
    }

    @ViewBuilder private var resultSection: some View {
        Section {
            switch job.outcome {
            case .succeeded(let newVersion)?:
                let version = newVersion.isEmpty ? (job.target?.toVersion ?? "") : newVersion
                Label {
                    Text("\(hostname) runs \(version)")
                } icon: {
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(.green)
                }
            case .failed(let error)?:
                Label { Text(verbatim: error) } icon: { Image(systemName: "xmark.octagon.fill").foregroundStyle(.red) }
            case .unfollowed?:
                Text("Stopped following. The node keeps upgrading: check its version later.")
                    .foregroundStyle(.secondary)
            case nil:
                EmptyView()
            }
            if job.isActive {
                Button("Stop following", role: .destructive) { job.stopFollowing() }
            } else {
                Button("Close") {
                    job.clear()
                    onClose()
                }
            }
        } footer: {
            if job.isActive {
                Text("The upgrade cannot be cancelled. Stopping only stops watching; the screen stays on meanwhile.")
            }
        }
    }
}

private struct StepRow: View {
    let step: UpgradeStep

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            icon.frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(step.phase.localizedLabel)
                        .fontWeight(step.state == .current ? .semibold : .regular)
                        .foregroundStyle(step.state == .pending ? Color.secondary : Color.primary)
                    Spacer()
                    if step.at > 0 {
                        Text(Date(timeIntervalSince1970: TimeInterval(step.at) / 1000), format: .dateTime.hour().minute().second())
                            .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                if !step.message.isEmpty {
                    Text(verbatim: step.message).font(.caption)
                        .foregroundStyle(step.state == .failed ? Color.red : Color.secondary)
                }
            }
        }
    }

    @ViewBuilder private var icon: some View {
        switch step.state {
        case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
        case .current: ProgressView()
        case .failed: Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
        case .pending: Image(systemName: "circle").foregroundStyle(.secondary)
        }
    }
}

extension UpgradePhase {
    var localizedLabel: String {
        switch self {
        case .requested: String(localized: "Requested")
        case .installing: String(localized: "Installing")
        case .rebooting: String(localized: "Rebooting")
        case .waiting: String(localized: "Waiting for node")
        case .booted: String(localized: "Booted")
        case .done: String(localized: "Done")
        }
    }
}
