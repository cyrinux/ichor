import SwiftUI
import IchorCore

/// One workload's recommendation: how a pod's requests change, each container's requests against
/// the limits CAST AI keeps, and how and whether it is applied.
struct CastAIWorkloadSheet: View {
    let rec: CastAIRecommendation

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section {
                    VStack(alignment: .leading, spacing: 8) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: rec.title).font(.headline.monospaced()).textSelection(.enabled)
                            Text(verbatim: rec.subtitle).font(.subheadline).foregroundStyle(.secondary)
                        }
                        pills
                    }
                    if !rec.message.isEmpty {
                        CastAIBanner(text: rec.message, bad: rec.health == .critical)
                            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                    }
                    if rec.nearMemoryLimit {
                        CastAIBanner(text: String(localized: "The memory request grows to \(String(rec.memoryLimitPercent))% of its limit. CAST AI keeps limits as they are, so the pod has little room before it is OOM-killed."))
                            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                    }
                }
                Section("Per pod") { perPod }
                if !rec.containers.isEmpty {
                    Section("Containers") {
                        ForEach(rec.containers) { CastAIContainerRow(container: $0) }
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private var pills: some View {
        let reasonColor: Color = rec.health == .critical ? .statusBad : .statusWarn
        return ChipFlow(spacing: 8) {
            if rec.reasons.isEmpty { StatusPill(label: String(localized: "Healthy"), color: .statusOK) }
            ForEach(rec.reasons, id: \.self) { StatusPill(label: $0.label, color: reasonColor) }
            if let mode = rec.mode.label { StatusPill(label: mode, color: .secondary) }
        }
    }

    /// A pod's CPU and memory requests, before and after, with the change.
    private var perPod: some View {
        let known = rec.change != .unknown
        return VStack(alignment: .leading, spacing: 16) {
            CastAIBigChange(title: String(localized: "CPU requests"),
                            before: formatMilliCores(rec.originalCpuMilli), after: formatMilliCores(rec.cpuMilli),
                            delta: known ? formatMilliCores(rec.cpuDeltaMilli, signed: true) : nil,
                            deltaColor: castAIDeltaColor(rec.cpuDeltaMilli),
                            beforeValue: Double(rec.originalCpuMilli), afterValue: Double(rec.cpuMilli))
            CastAIBigChange(title: String(localized: "Memory requests"),
                            before: formatBytes(rec.originalMemoryBytes), after: formatBytes(rec.memoryBytes),
                            delta: known ? signedBytes(rec.memoryDeltaBytes) : nil,
                            deltaColor: castAIDeltaColor(rec.memoryDeltaBytes),
                            beforeValue: Double(rec.originalMemoryBytes), afterValue: Double(rec.memoryBytes))
        }
        .padding(.vertical, 6)
    }
}

private struct CastAIBigChange: View {
    let title: String
    let before: String
    let after: String
    let delta: String?
    let deltaColor: Color
    let beforeValue: Double
    let afterValue: Double

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .lastTextBaseline) {
                Text(verbatim: title).font(.subheadline.weight(.medium)).foregroundStyle(.secondary)
                Spacer()
                if let delta { Text(verbatim: delta).font(.subheadline.monospaced()).foregroundStyle(deltaColor) }
            }
            HStack(alignment: .lastTextBaseline, spacing: 8) {
                if before != after {
                    Text(verbatim: before).font(.headline.monospaced()).foregroundStyle(.secondary)
                    Text(verbatim: "→").foregroundStyle(.secondary).accessibilityHidden(true)
                }
                Text(verbatim: after).font(.title2.monospaced())
            }
            CastAIBeforeAfterBar(before: beforeValue, after: afterValue, height: 10)
        }
    }
}

/// One container: its requests before and after, and how much of each limit the new request takes.
private struct CastAIContainerRow: View {
    let container: CastAIContainer

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: container.name).font(.subheadline.monospaced())
            line(String(localized: "CPU"), change: castAIChangeText(container.originalCpu.or(container.cpu), container.cpu),
                 limit: container.cpuLimit, percent: container.cpuLimitPercent, warn: false)
            line(String(localized: "Memory"), change: castAIChangeText(container.originalMemory.or(container.memory), container.memory),
                 limit: container.memoryLimit, percent: container.memoryLimitPercent, warn: container.nearMemoryLimit)
        }
        .padding(.vertical, 2)
    }

    private func line(_ label: String, change: String, limit: String, percent: Int, warn: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verbatim: label).font(.caption).foregroundStyle(.secondary).frame(width: 64, alignment: .leading)
            Text(verbatim: change.or("—")).font(.caption.monospaced()).frame(maxWidth: .infinity, alignment: .leading)
            Text(verbatim: limit.isEmpty ? String(localized: "no limit") : String(localized: "limit \(limit) · \(String(percent))%"))
                .font(.caption2)
                .foregroundStyle(warn ? Color.statusWarn : Color.secondary)
        }
    }
}
