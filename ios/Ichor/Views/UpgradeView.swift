import SwiftUI
import IchorCore

/// `talosctl upgrade` of one node (os:admin): plan with blockers, warnings and risks to
/// acknowledge, target version and image, then the progress until the node is back. One
/// upgrade at a time in the app.
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
    /// UpgradeVersionCheck for the target: nil while it is computed.
    @State private var versionRisk: String?
    @State private var stage = false
    /// Cordon and drain first (a maintenance run), when the plan says the upgrade does not.
    @State private var drain = true
    @State private var force = false
    @State private var confirmForce = false
    @State private var confirming = false
    @State private var confirmingRollback = false
    @State private var message: String?
    @State private var showingPull = false

    private var job: UpgradeJob { .shared }

    var body: some View {
        Group {
            if let target = job.target, target.node == node {
                UpgradeProgressView(job: job, hostname: hostname) { Task { await loadPlan() } }
            } else if let run = MaintenanceJob.shared.target, run.node == node, run.action == .upgrade {
                MaintenanceRunView(job: .shared, hostname: hostname) { Task { await loadPlan() } }
            } else {
                LoadStateView(state: plan, retry: loadPlan) { plan in form(plan) }
            }
        }
        .navigationTitle(String(localized: "Upgrade · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadPlan() }
        .task { await loadReleases() }
        .sheet(isPresented: $showingPull) { ImagePullSheet() }
        // The node runs another Talos version now: what it can do is asked again.
        .onChange(of: job.outcome) { _, outcome in
            if case .succeeded? = outcome, let target = job.target {
                Task { await model.reloadFeatures(node: target.node) }
            }
        }
        .task(id: [version, loadedPlan?.currentImage ?? ""]) { await computeImage() }
    }

    /// Pulls the installer on every node (another pull already running is shown instead).
    private func prePull() {
        if let client = model.client {
            ImagePullJob.shared.start(client: client, image: image, namespace: .system)
        }
        showingPull = true
    }

    private var loadedPlan: UpgradePlan? {
        if case .loaded(let plan, _, _) = plan { return plan }
        return nil
    }

    private func makeGate(_ plan: UpgradePlan, acknowledged: Bool = false) -> UpgradeGate {
        UpgradeGate(plan: plan, targetVersion: version, versionRisk: versionRisk ?? "", force: force, busy: job.isActive,
                    acknowledged: acknowledged)
    }

    private func form(_ plan: UpgradePlan) -> some View {
        let gate = makeGate(plan)
        return Form {
            if job.isActive, let other = job.target {
                Section {
                    Text("An upgrade of \(other.hostname) is in progress. The app runs one upgrade at a time.")
                        .foregroundStyle(.statusWarn)
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
                .disabled(!gate.canRequest || image.isEmpty || versionRisk == nil)
                if let message { Text(message).font(.footnote).foregroundStyle(.statusBad) }
            } footer: {
                Text("The node reboots into the new version. An upgrade cannot be cancelled once requested.")
            }
        }
        .themedBackground()
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    if gate.forceAvailable {
                        Button(role: .destructive) { confirmForce = true } label: {
                            Label("Force upgrade…", systemImage: "exclamationmark.triangle")
                        }
                    }
                    Button(role: .destructive) { Task { await requestRollback() } } label: {
                        Label(CheckupText.rollbackMenu, systemImage: "arrow.uturn.backward")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                }
            }
        }
        .sheet(isPresented: $confirmingRollback) {
            HostnameConfirmationSheet(
                title: CheckupText.rollbackTitle(hostname),
                message: CheckupText.rollbackBody,
                hostname: hostname,
                actionTitle: CheckupText.rollbackConfirm
            ) {
                confirmingRollback = false
                rollback()
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
                actionTitle: stage ? String(localized: "Stage upgrade") : String(localized: "Start upgrade"),
                acknowledgments: gate.acknowledgments
            ) {
                confirming = false
                start(plan)
            }
        }
    }

    private func currentSection(_ plan: UpgradePlan) -> some View {
        Section("Current") {
            LabeledContent("Version", value: plan.currentVersion.or("—"))
            LabeledContent("Role", value: plan.controlPlane ? String(localized: "Control plane") : String(localized: "Worker"))
            VStack(alignment: .leading, spacing: 2) {
                Text("Image").font(.caption).foregroundStyle(.secondary)
                Text(verbatim: plan.currentImage.or("—"))
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
                Text("Not a Talos version (vMAJOR.MINOR.PATCH).").font(.footnote).foregroundStyle(.statusBad)
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
                // Pulling the installer on every node first keeps each node's reboot short.
                Button("Pre-pull the image on all nodes") { prePull() }
                Text("Download it on every node now, so each node's reboot is short.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            // The upgrade path that does not drain the node has no staged upgrade either.
            if plan.drainable {
                Toggle(isOn: $drain) {
                    VStack(alignment: .leading) {
                        Text("Drain the node first")
                        Text("Cordon and drain it, upgrade, wait until it is back and Ready, then uncordon it. Without it, this Talos version stops the node's pods when it reboots.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
            } else {
                Toggle(isOn: $stage) {
                    VStack(alignment: .leading) {
                        Text("Stage (--stage)")
                        Text("Installs during the reboot instead of before it, for nodes whose files in use block the upgrade.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("Target")
        }
    }

    @ViewBuilder private func checksSection(_ plan: UpgradePlan, gate: UpgradeGate) -> some View {
        if !plan.blockers.isEmpty || !plan.warnings.isEmpty || !gate.acknowledgments.isEmpty || gate.downgrade || gate.sameVersion || force {
            Section("Checks") {
                ForEach(plan.blockers, id: \.self) { blocker in
                    Label { Text(verbatim: blocker) } icon: { Image(systemName: "xmark.octagon.fill") }
                        .foregroundStyle(.statusBad)
                }
                ForEach(plan.warnings, id: \.self) { warning in
                    Label { Text(verbatim: warning) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                        .foregroundStyle(.statusWarn)
                }
                // Confirmed one by one in the confirmation sheet.
                ForEach(gate.acknowledgments, id: \.self) { risk in
                    Label { Text(verbatim: risk) } icon: { Image(systemName: "hand.raised.fill") }
                        .foregroundStyle(.statusWarn)
                }
                // Go's version risk already says it.
                if gate.downgrade && (versionRisk ?? "").isEmpty {
                    Label("This is a downgrade: not every Talos version supports going back.", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.statusWarn)
                }
                if gate.sameVersion {
                    Label("This node already runs this version: the image is reinstalled.", systemImage: "info.circle")
                        .foregroundStyle(.secondary)
                }
                if force {
                    HStack {
                        Label("Force: etcd checks skipped", systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.statusBad).fontWeight(.semibold)
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
        text += "\n\n" + String(localized: "Keep Ichor open until the node reboots: in the background iOS can pause the app, and the upgrade with it.")
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
        versionRisk = nil
        guard let plan = loadedPlan, let target = normalizedTalosVersion(version) else {
            image = ""
            versionRisk = ""
            return
        }
        let risk = await TalosClient.versionRisk(from: plan.currentVersion, to: target)
        guard !Task.isCancelled else { return }
        versionRisk = risk
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

    /// The same two steps as an upgrade: the node reboots at once, into another Talos.
    private func requestRollback() async {
        message = nil
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: CheckupText.rollbackTitle(hostname)) {
            message = failure
            return
        }
        confirmingRollback = true
    }

    private func rollback() {
        guard let client = model.client else { return }
        Task {
            do {
                try await client.rollback(node: node)
                message = CheckupText.rollbackStarted(hostname)
            } catch {
                message = error.localizedDescription
            }
        }
    }

    /// After the confirmation sheet, which collected the acknowledgments.
    private func start(_ plan: UpgradePlan) {
        let gate = makeGate(plan, acknowledged: true)
        guard let client = model.client, let target = normalizedTalosVersion(version), gate.canStart, !image.isEmpty,
              versionRisk != nil else { return }
        if plan.drainable && drain {
            guard !MaintenanceJob.shared.isActive else {
                message = String(localized: "Another maintenance is running. The app runs one at a time.")
                return
            }
            MaintenanceJob.shared.start(client: client,
                                        target: MaintenanceJob.Target(node: node, hostname: hostname, action: .upgrade, wasCordoned: false,
                                                                      image: image, force: force && gate.forceAvailable),
                                        includeBare: false, acknowledged: !gate.acknowledgments.isEmpty)
            return
        }
        job.start(client: client, target: UpgradeJob.Target(node: node, hostname: hostname, fromVersion: plan.currentVersion,
                                                           toVersion: target, image: image, stage: stage),
                  force: force && gate.forceAvailable, acknowledged: !gate.acknowledgments.isEmpty)
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
            if job.needsForeground {
                Section {
                    Label {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("Keep Ichor open until the node reboots").fontWeight(.semibold)
                            Text("Until the reboot Ichor may drive the download and install: in the background iOS pauses it, and the upgrade can stall.")
                                .font(.footnote).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "iphone").foregroundStyle(.orange)
                    }
                }
            }
            if let target = job.target {
                Section {
                    LabeledContent("Version", value: "\(target.fromVersion.or("—")) → \(target.toVersion)")
                    Text(verbatim: target.image).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
            Section("Progress") {
                ForEach(job.timeline) { step in
                    RunStepRow(label: step.phase.localizedLabel, state: step.state, at: step.at, message: step.message)
                }
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
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(.statusOK)
                }
            case .failed(let error)?:
                Label { Text(verbatim: error) } icon: { Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad) }
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
