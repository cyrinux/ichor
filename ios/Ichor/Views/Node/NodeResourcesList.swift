import SwiftUI
import IchorCore

struct NodeServiceRow: View {
    let service: ServiceInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                VStack(alignment: .leading) {
                    Text(service.id).font(.headline)
                    Text(service.state).font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                switch service.health {
                case "healthy": StatusPill(label: String(localized: "Healthy"), color: .green)
                case "unhealthy": StatusPill(label: String(localized: "Unhealthy"), color: .red)
                default: StatusPill(label: String(localized: "No check"), color: .gray)
                }
            }
            let detail = service.health == "unhealthy" ? service.message : service.lastEvent
            if let detail, !detail.isEmpty {
                Text(detail).font(.caption).foregroundStyle(service.health == "unhealthy" ? .red : .secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

struct ResourcesList: View {
    let resources: NodeResources
    let clock: NodeTimeInfo?
    /// nil when the role cannot read cgroups: no pressure section then.
    let pressure: LoadState<CgroupReport>?
    /// Memory used (%) over the history's window; a line under two points is left out.
    let memory: [HistoryPoint]
    let window: ClosedRange<Int64>
    let onPressureDetails: () -> Void

    /// System and Memory always; Pressure and Disks when shown.
    private var jumpSections: [JumpSection] {
        [
            JumpSection(id: "system", label: String(localized: "System")),
            JumpSection(id: "memory", label: String(localized: "Memory")),
            pressure == nil ? nil : JumpSection(id: "pressure", label: String(localized: "Pressure")),
            resources.mounts.isEmpty ? nil : JumpSection(id: "disks", label: String(localized: "Disks")),
        ].compactMap { $0 }
    }

    var body: some View {
        SectionRailScroll(sections: jumpSections) {
            List {
                Section {
                    LabeledContent("Uptime", value: uptime)
                    LabeledContent(
                        "CPU",
                        value: resources.cpuModel.isEmpty
                            ? String(localized: "\(resources.cpuCount) threads")
                            : "\(resources.cpuCount) × \(resources.cpuModel)")
                    LabeledContent(
                        "Load (1/5/15)",
                        value: String(format: "%.2f  %.2f  %.2f", resources.load1, resources.load5, resources.load15))
                    if resources.cpuCount > 0 { UsageBar(fraction: resources.load1 / Double(resources.cpuCount)) }
                    if let clock { TimeOffsetRow(info: clock, title: String(localized: "Clock offset")) }
                } header: {
                    Text("System").sectionTarget("system")
                }
                Section {
                    let used = resources.memTotal - min(resources.memAvailable, resources.memTotal)
                    LabeledContent("Used", value: "\(formatBytes(used)) / \(formatBytes(resources.memTotal))")
                    UsageBar(fraction: usedFraction(total: resources.memTotal, available: resources.memAvailable))
                    if memory.count >= 2 {
                        HistorySparkline(
                            points: memory, from: window.lowerBound, to: window.upperBound,
                            label: Text("Memory used over the period"))
                    }
                } header: {
                    Text("Memory").sectionTarget("memory")
                }
                // Right after memory: pressure says whether CPU, memory or disk actually hold tasks back.
                if let pressure { PressureSection(state: pressure, onDetails: onPressureDetails) }
                if !resources.mounts.isEmpty {
                    Section {
                        ForEach(resources.mounts) { mount in
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text(mount.mountedOn)
                                    Spacer()
                                    Text(
                                        verbatim:
                                            "\(formatBytes(mount.size - min(mount.available, mount.size))) / \(formatBytes(mount.size))"
                                    ).font(.caption)
                                }
                                Text(mount.filesystem).font(.caption.monospaced()).foregroundStyle(.secondary)
                                UsageBar(fraction: usedFraction(total: mount.size, available: mount.available))
                            }
                        }
                    } header: {
                        Text("Disks").sectionTarget("disks")
                    }
                }
            }
            .themedBackground()
        }
    }

    private var uptime: String {
        guard resources.bootTime > 0 else { return "—" }
        return localizedDuration(Int64(Date().timeIntervalSince1970) - Int64(resources.bootTime))
    }
}
