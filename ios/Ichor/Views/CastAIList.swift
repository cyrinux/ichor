import SwiftUI
import IchorCore

/// CAST AI: the Workload Autoscaler's recommendations and, on clusters where CAST AI drives
/// Karpenter, its node consolidations. Without any consolidation the switch is left out.
struct CastAIList: View {
    let status: CastAIStatus
    let refresh: () async -> Void

    private enum Pane: Hashable { case workloads, nodes }

    @State private var pane: Pane = .workloads

    var body: some View {
        if status.plans.isEmpty && status.plansError.isEmpty {
            CastAIWorkloadsList(status: status, refresh: refresh)
        } else {
            Group {
                switch pane {
                case .workloads: CastAIWorkloadsList(status: status, refresh: refresh)
                case .nodes: CastAIPlansList(status: status, refresh: refresh)
                }
            }
            .safeAreaInset(edge: .top, spacing: 0) {
                Picker(selection: $pane) {
                    Text("Workloads · \(status.recommendations.count)").tag(Pane.workloads)
                    Text("Nodes · \(status.plans.count)").tag(Pane.nodes)
                } label: {
                    EmptyView()
                }
                .pickerStyle(.segmented)
                .padding(.horizontal)
                .padding(.vertical, 6)
                .background(.bar)
            }
        }
    }
}

/// The Workload Autoscaler's recommendations, biggest changes first: what needs a look, then the
/// workloads whose requests grow (OOM and scheduling risk), then the savings. Each row draws the
/// requests before CAST AI against the recommended ones; a tap opens the containers.
struct CastAIWorkloadsList: View {
    let status: CastAIStatus
    let refresh: () async -> Void

    @State private var mode: CastAIListMode = .overview
    @State private var query = ""
    @State private var selected: CastAIRecommendation?

    var body: some View {
        let sections = status.sections(mode, query: query)
        let growCount = status.recommendations.filter { $0.change == .grow }.count
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            if status.recommendations.isEmpty {
                Section { Text("No CAST AI recommendations.").foregroundStyle(.secondary) }
            } else {
                Section { CastAIWorkloadsSummary(status: status) }
                Section {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            chip(String(localized: "Overview"), .overview)
                            chip(String(localized: "Grows · \(growCount)"), .grows)
                            chip(String(localized: "By namespace"), .namespaces)
                        }
                    }
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 16))
                    .listRowBackground(Color.clear)
                }
                if sections.isEmpty {
                    Section { Text("No workload matches.").foregroundStyle(.secondary) }
                }
                ForEach(sections) { section in
                    Section {
                        ForEach(section.rows) { rec in
                            Button { selected = rec } label: { CastAIWorkloadRow(rec: rec) }
                                .buttonStyle(.plain)
                        }
                        if section.hidden > 0 {
                            Button("All \(section.total) that grow") { mode = .grows }
                        }
                    } header: {
                        CastAISectionHeader(section: section)
                    }
                }
            }
        }
        .searchable(text: $query, prompt: Text("Filter by namespace or workload"))
        .refreshable { await refresh() }
        .themedBackground()
        .sheet(item: $selected) { CastAIWorkloadSheet(rec: $0) }
    }

    private func chip(_ label: String, _ value: CastAIListMode) -> some View {
        Button { mode = value } label: {
            Text(verbatim: label)
                .font(.subheadline.weight(.medium))
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .foregroundStyle(mode == value ? AnyShapeStyle(Color.white) : AnyShapeStyle(HierarchicalShapeStyle.primary))
                .background(mode == value ? AnyShapeStyle(TintShapeStyle.tint) : AnyShapeStyle(Color(.tertiarySystemFill)), in: Capsule())
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(mode == value ? .isSelected : [])
    }
}

/// A section's title in its colour, with its count, or a namespace's CPU and memory change.
private struct CastAISectionHeader: View {
    let section: CastAISection

    var body: some View {
        HStack(alignment: .lastTextBaseline) {
            Text(verbatim: title).foregroundStyle(color)
            Spacer()
            Text(verbatim: note).font(.caption.monospaced()).foregroundStyle(.secondary)
        }
        // A namespace is shown as written.
        .textCase(nil)
    }

    private var title: String {
        switch section.kind {
        case .attention: String(localized: "Needs a look")
        case .grows: String(localized: "Grows — check the limits")
        case .reductions: String(localized: "Largest reductions")
        case .other: String(localized: "Unchanged or no baseline")
        case .namespace: section.namespace
        }
    }

    private var color: Color {
        switch section.kind {
        case .attention: .statusBad
        case .grows: .statusWarn
        case .reductions: .statusOK
        case .other: .secondary
        case .namespace: .primary
        }
    }

    private var note: String {
        guard section.kind == .namespace else { return "\(section.total)" }
        return String(localized: "\(formatMilliCores(section.cpuDeltaMilli, signed: true)) CPU · \(signedBytes(section.memoryDeltaBytes))")
    }
}

/// What CAST AI changes across the cluster, which way each workload goes and how it is applied.
private struct CastAIWorkloadsSummary: View {
    let status: CastAIStatus

    var body: some View {
        let counts = status.changeCounts
        VStack(alignment: .leading, spacing: 14) {
            Text("Requests if every recommendation applies").font(.subheadline.weight(.medium)).foregroundStyle(.secondary)
            if status.compared > 0 {
                HStack(alignment: .top, spacing: 12) {
                    CastAIMetricTile(title: String(localized: "CPU requests"), value: formatMilliCores(status.cpuDeltaMilli, signed: true),
                                     caption: String(localized: "cores"), color: castAIDeltaColor(status.cpuDeltaMilli))
                    CastAIMetricTile(title: String(localized: "Memory requests"), value: signedBytes(status.memoryDeltaBytes),
                                     caption: String(localized: "Per pod"), color: castAIDeltaColor(status.memoryDeltaBytes))
                }
            }
            CastAISplitBar(parts: [
                CastAISplitPart(count: counts.shrink, color: .statusOK, label: String(localized: "\(counts.shrink) shrink")),
                CastAISplitPart(count: counts.grow, color: .statusWarn, label: String(localized: "\(counts.grow) grow")),
                CastAISplitPart(count: counts.same, color: .gray, label: String(localized: "\(counts.same) unchanged")),
                CastAISplitPart(count: counts.unknown, color: Color(.systemFill), label: String(localized: "\(counts.unknown) without a baseline")),
            ])
            if let common = status.commonMode, let label = common.mode.label {
                Text(verbatim: String(localized: "\(label) · \(String(common.count)) of \(String(status.recommendations.count)) workloads"))
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 6)
    }
}

/// Name, the deltas, a bar per resource, and why it needs a look if it does.
private struct CastAIWorkloadRow: View {
    let rec: CastAIRecommendation

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 1) {
                    Text(verbatim: rec.title).font(.subheadline.monospaced()).lineLimit(1).truncationMode(.middle)
                    Text(verbatim: rec.subtitle).font(.caption2).foregroundStyle(.secondary)
                }
                Spacer()
                if rec.change != .unknown {
                    VStack(alignment: .trailing, spacing: 1) {
                        Text(verbatim: formatMilliCores(rec.cpuDeltaMilli, signed: true)).foregroundStyle(castAIDeltaColor(rec.cpuDeltaMilli))
                        Text(verbatim: signedBytes(rec.memoryDeltaBytes)).foregroundStyle(castAIDeltaColor(rec.memoryDeltaBytes))
                    }
                    .font(.caption.monospaced())
                }
            }
            CastAIResourceRow(label: String(localized: "CPU"), before: Double(rec.originalCpuMilli), after: Double(rec.cpuMilli),
                              text: castAIChangeText(formatMilliCores(rec.originalCpuMilli), formatMilliCores(rec.cpuMilli)))
            CastAIResourceRow(label: String(localized: "Memory"), before: Double(rec.originalMemoryBytes), after: Double(rec.memoryBytes),
                              text: castAIChangeText(formatBytes(rec.originalMemoryBytes), formatBytes(rec.memoryBytes)))
            if rec.health.needsAttention {
                Text(verbatim: (rec.reasons.map(\.label) + [rec.message].filter { !$0.isEmpty }).joined(separator: " · "))
                    .font(.caption2)
                    .foregroundStyle(rec.health == .critical ? Color.statusBad : Color.statusWarn)
            }
            if rec.nearMemoryLimit {
                Text(verbatim: String(localized: "Memory request at \(String(rec.memoryLimitPercent))% of its limit"))
                    .font(.caption2).foregroundStyle(.statusWarn)
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }
}
