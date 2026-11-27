import SwiftUI
import IchorCore

/// The Hardware screen's CPU scaling section (Talos 1.15): one row per cpufreq policy with its
/// governor, the CPUs it drives, the frequency window and the CPUScalingConfig that set it. A
/// hint when a powersave policy meets a throttled node. Hidden by the caller when empty.
struct CPUScalingSection: View {
    let hardware: NodeHardware
    let throttled: Bool

    var body: some View {
        Section {
            SectionError(message: hardware.errors["cpuScaling"])
            if hardware.powersaveWhileThrottled(throttled) {
                Text(
                    "The powersave governor holds these cores back on a node that is already throttled. A CPUScalingConfig with the performance governor keeps them at speed."
                )
                .font(.caption)
                .foregroundStyle(Color.statusWarn)
            }
            ForEach(hardware.cpuScaling) { policy in
                PolicyRow(policy: policy)
            }
        } header: {
            Text("CPU scaling").sectionTarget("cpuScaling")
        }
    }
}

private struct PolicyRow: View {
    let policy: CPUScalingPolicy

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: [policy.policy, policy.cpus].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.body.monospaced())
                Spacer()
                if !policy.coreType.isEmpty {
                    StatusPill(label: policy.coreType, color: .secondary)
                }
                if !policy.governor.isEmpty {
                    StatusPill(label: policy.governor, color: policy.isPowersave ? .statusWarn : .accentColor)
                }
            }
            if !details.isEmpty {
                Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
            }
            if !policy.configuredBy.isEmpty {
                Text("Set by \(policy.configuredBy)").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private var details: String {
        var parts: [String] = []
        if policy.maxKhz > 0 { parts.append("\(policy.minKhz / 1000)–\(policy.maxKhz / 1000) MHz") }
        if !policy.energyPreference.isEmpty { parts.append("EPP \(policy.energyPreference)") }
        if !policy.driver.isEmpty { parts.append(policy.driver) }
        return parts.joined(separator: "  ·  ")
    }
}

/// The Hardware screen's NUMA section (Talos 1.15): one row per node with its CPUs and memory.
/// Hidden by the caller on a machine with a single node.
struct NUMASection: View {
    let hardware: NodeHardware

    var body: some View {
        Section {
            SectionError(message: hardware.errors["numa"])
            ForEach(hardware.numa) { node in
                HStack {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Node \(node.id)")
                        Text("CPUs: \(node.cpuCount)").font(.caption).foregroundStyle(.secondary)
                        Text(verbatim: node.cpus).font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                    Spacer()
                    if node.memoryBytes > 0 {
                        Text(verbatim: ByteCountFormatter.string(fromByteCount: node.memoryBytes, countStyle: .memory))
                            .monospacedDigit()
                    }
                }
            }
        } header: {
            Text("NUMA").sectionTarget("numa")
        }
    }
}
