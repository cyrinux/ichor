import SwiftUI
import IchorCore

/// The cluster checkup (os:admin): what no other screen shows, one group per kind of trouble.
/// A group opens on its findings, each worded with what to do; the capacity, storage, nodes and
/// Helm groups also list what they measured. Reading it lists the cluster's pods and asks every
/// kubelet: loaded on demand, never polled.
struct CheckupView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<CheckupReport> = .loading
    /// The sections shown open; those in trouble until the user chooses.
    @State private var open: Set<String> = []
    @State private var chosen = false

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            let now = Int64(Date().timeIntervalSince1970 * 1000)
            List {
                CheckupVerdictSection(report: report)
                ForEach(report.shownSections) { section in
                    Section {
                        DisclosureGroup(isExpanded: expanded(section.id)) {
                            CheckupSectionBody(section: section, report: report, now: now)
                        } label: {
                            CheckupSectionLabel(section: section)
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(Text(verbatim: CheckupText.checkupTitle))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func expanded(_ id: String) -> Binding<Bool> {
        Binding(
            get: { open.contains(id) },
            set: { on in
                chosen = true
                if on { open.insert(id) } else { open.remove(id) }
            }
        )
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<CheckupReport> = await .from { try await client.checkup() }
        if case .loaded(let report, _, _) = loaded, !chosen {
            open = Set(report.shownSections.filter { $0.status == .critical || $0.status == .warning }.map(\.id))
        }
        state = state.refreshed(with: loaded)
    }
}

/// The verdict, how many findings of each weight, and what the checkup is.
private struct CheckupVerdictSection: View {
    let report: CheckupReport

    private var label: String {
        switch report.status {
        case .critical: CheckupText.checkupVerdictCritical
        case .warning: CheckupText.checkupVerdictWarning
        default: CheckupText.checkupVerdictOk
        }
    }

    var body: some View {
        let critical = report.count(.critical)
        let warnings = report.count(.warning)
        Section {
            HStack {
                StatusPill(label: label, color: report.status == .unknown ? CheckupStatus.ok.color : report.status.color)
                Spacer()
                if !report.kubeVersion.isEmpty {
                    Text(verbatim: "Kubernetes \(report.kubeVersion)").font(.caption.monospaced()).foregroundStyle(.secondary)
                }
            }
            Text(verbatim: critical + warnings == 0
                ? CheckupText.checkupAllClear("\(report.shownSections.count)")
                : CheckupText.checkupCounts("\(critical)", "\(warnings)"))
                .font(.callout)
        } footer: {
            Text(verbatim: CheckupText.checkupIntro)
        }
    }
}

/// A section's header: its icon in its status colour, its name, what it looks for and its counts.
private struct CheckupSectionLabel: View {
    let section: CheckupSection

    var body: some View {
        let critical = section.count(.critical)
        let warnings = section.count(.warning)
        let notes = section.findings.count - critical - warnings
        HStack(spacing: 10) {
            Image(systemName: section.section?.symbol ?? "questionmark.circle")
                .foregroundStyle(section.status.color)
                .frame(width: 26)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: section.section?.title ?? section.id).font(.subheadline.weight(.semibold))
                if let hint = section.section?.hint {
                    Text(verbatim: hint).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                }
            }
            Spacer(minLength: 4)
            if critical > 0 { CheckupCount(count: critical, color: Color.statusBad) }
            if warnings > 0 { CheckupCount(count: warnings, color: Color.statusWarn) }
            if notes > 0 { CheckupCount(count: notes, color: .secondary) }
            if section.findings.isEmpty {
                if section.status == .unknown {
                    Text(verbatim: "?").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                } else {
                    Image(systemName: "checkmark.circle").foregroundStyle(Color.statusOK)
                        .accessibilityLabel(Text(verbatim: CheckupText.checkupNothingFound))
                }
            }
        }
    }
}

private struct CheckupCount: View {
    let count: Int
    let color: Color

    var body: some View {
        Text(verbatim: "\(count)")
            .font(.caption.weight(.semibold).monospacedDigit())
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .foregroundStyle(color)
            .background(color.opacity(0.15), in: Capsule())
    }
}

/// A section's findings, then what it measured: node requests, volume levels, taints, releases.
private struct CheckupSectionBody: View {
    let section: CheckupSection
    let report: CheckupReport
    let now: Int64

    var body: some View {
        if !section.error.isEmpty {
            ErrorOrNoticeText(message: CheckupText.checkupSectionError(section.error))
        }
        if section.findings.isEmpty && section.error.isEmpty {
            Text(verbatim: CheckupText.checkupNothingIn("\(section.checked)")).font(.caption).foregroundStyle(.secondary)
        }
        ForEach(Array(section.findings.enumerated()), id: \.offset) { _, finding in
            CheckupFindingRow(finding: finding, now: now)
        }
        if section.truncated > 0 {
            Text(verbatim: CheckupText.checkupTruncated("\(section.truncated)")).font(.caption).foregroundStyle(.secondary)
        }
        switch section.section {
        case .capacity:
            let nodes = report.nodes.sorted { max($0.cpuPercent, $0.memoryPercent) > max($1.cpuPercent, $1.memoryPercent) }
            CheckupRows(title: CheckupText.checkupRequestsTitle, rows: nodes) { CheckupNodeRequests(node: $0) }
        case .nodes:
            CheckupRows(title: CheckupText.checkupTaintsTitle, rows: report.nodes) { CheckupNodeTaints(node: $0) }
        case .storage:
            CheckupRows(title: CheckupText.checkupVolumesTitle, rows: report.volumes) { CheckupVolumeRow(volume: $0) }
        case .helm:
            CheckupRows(title: CheckupText.checkupReleasesTitle, rows: report.releases) { CheckupReleaseRow(release: $0, now: now) }
        default:
            EmptyView()
        }
    }
}

/// One problem: what it is about, what happens, Kubernetes' own words and what to do.
private struct CheckupFindingRow: View {
    let finding: CheckupFinding
    let now: Int64

    private var subject: String {
        finding.kind == .event && !finding.extra.isEmpty ? "\(finding.extra) \(finding.subject)" : finding.subject
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: finding.severity.symbol).foregroundStyle(finding.severity.color).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: subject).font(.subheadline.monospaced().weight(.semibold)).lineLimit(2).truncationMode(.middle)
                Text(verbatim: finding.title(now: now)).font(.callout)
                if !finding.node.isEmpty {
                    Text(verbatim: CheckupText.checkupOnNode(finding.node)).font(.caption).foregroundStyle(.secondary)
                }
                if !finding.detail.isEmpty {
                    Text(verbatim: finding.detail).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(6)
                }
                if let fix = finding.fix {
                    Text(verbatim: fix).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 2)
    }
}

/// A measured list cut to its first rows, with a button for the rest.
private struct CheckupRows<Item: Identifiable, Row: View>: View {
    let title: String
    let rows: [Item]
    @ViewBuilder let row: (Item) -> Row
    @State private var all = false

    private let preview = 6

    var body: some View {
        if !rows.isEmpty {
            Text(verbatim: title).font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
            ForEach(all ? rows : Array(rows.prefix(preview))) { row($0) }
            if rows.count > preview {
                Button {
                    all.toggle()
                } label: {
                    Text(verbatim: all ? CheckupText.checkupShowLess : CheckupText.checkupShowAll("\(rows.count)")).font(.callout)
                }
            }
        }
    }
}

/// What a node's pods request of what it offers: the scheduler's view, not live usage.
private struct CheckupNodeRequests: View {
    let node: CheckupNode

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: node.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: CheckupText.checkupPodsOf("\(node.pods)", "\(node.podCapacity)")).font(.caption).foregroundStyle(.secondary)
            }
            bar(CheckupText.checkupCpu, percent: node.cpuPercent,
                amount: CheckupText.checkupCoresOf(String(format: "%.1f", node.cpuRequests), String(format: "%.1f", node.cpuAllocatable)))
            bar(CheckupText.checkupMemory, percent: node.memoryPercent,
                amount: "\(formatBytes(Int64(node.memoryRequests))) / \(formatBytes(Int64(node.memoryAllocatable)))")
        }
        .padding(.vertical, 2)
    }

    private func bar(_ label: String, percent: Double, amount: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: label).font(.caption).foregroundStyle(.secondary)
                Spacer()
                Text(verbatim: "\(checkupPercent(percent.rounded()))  \(amount)").font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            UsageBar(fraction: percent / 100)
        }
    }
}

/// A node's roles, taints and labels: what keeps pods away from it or draws them to it.
private struct CheckupNodeTaints: View {
    let node: CheckupNode
    @State private var showLabels = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: node.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                if !node.kubelet.isEmpty { Text(verbatim: node.kubelet).font(.caption.monospaced()).foregroundStyle(.secondary) }
            }
            Text(verbatim: summary).font(.caption).foregroundStyle(node.ready && !node.cordoned ? Color.secondary : Color.statusWarn)
            ForEach(node.taints, id: \.self) { taint in
                Text(verbatim: taint).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            }
            if !node.labels.isEmpty {
                Button {
                    showLabels.toggle()
                } label: {
                    Text(verbatim: showLabels ? CheckupText.checkupShowLess : CheckupText.checkupLabels("\(node.labels.count)")).font(.caption)
                }
                .buttonStyle(.borderless)
                if showLabels {
                    ForEach(node.labels, id: \.self) { label in
                        Text(verbatim: label).font(.caption2.monospaced()).foregroundStyle(.secondary)
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }

    /// "worker · cordoned", or "no taint" for a node nothing keeps pods away from.
    private var summary: String {
        var parts = node.roles
        if node.cordoned { parts.append(CheckupText.checkupCordoned) }
        if node.taints.isEmpty { parts.append(CheckupText.checkupNoTaint) }
        return parts.joined(separator: " · ")
    }
}

/// A claim with its level; an unmeasured one says so.
private struct CheckupVolumeRow: View {
    let volume: CheckupVolume

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: volume.id).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: volume.measured
                    ? "\(formatBytes(Int64(volume.used))) / \(formatBytes(Int64(volume.capacity)))"
                    : formatBytes(Int64(volume.capacity)))
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            if volume.measured {
                UsageBar(fraction: volume.usedPercent / 100)
            } else {
                Text(verbatim: volume.phase == "Bound" ? CheckupText.checkupVolumeUnmeasured : volume.phase)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}

/// A Helm release at its latest revision.
private struct CheckupReleaseRow: View {
    let release: CheckupRelease
    let now: Int64

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: release.id).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Text(verbatim: CheckupText.checkupReleaseRevision("\(release.revision)", checkupAge(release.updated, now: now)))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            InfoChip(text: release.status, color: release.inTrouble ? Color.statusWarn : Color.statusOK)
        }
    }
}
