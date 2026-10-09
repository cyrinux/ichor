import SwiftUI
import IchorCore

/// `kubectl top` on the Kubernetes screens: CPU and memory in use from metrics-server, as bars
/// on node and pod rows. Without metrics-server the rows simply show no bars.

/// A node's CPU and memory in use against what it can give.
struct NodeUsageView: View {
    let top: KubeTopNode

    var body: some View {
        VStack(spacing: 2) {
            UsageLine(label: String(localized: "CPU"),
                      value: "\(formatCPU(top.cpu)) / \(formatCPU(top.cpuAllocatable))",
                      fraction: top.cpuPercent / 100)
            UsageLine(label: String(localized: "Memory"),
                      value: "\(formatBytes(Int64(top.memory))) / \(formatBytes(Int64(top.memoryAllocatable)))",
                      fraction: top.memoryPercent / 100)
        }
    }
}

/// A pod's CPU and memory in use against its limit, else its request (no bar when neither).
struct PodUsageView: View {
    let top: KubeTopPod

    var body: some View {
        VStack(spacing: 2) {
            UsageLine(label: String(localized: "CPU"),
                      value: formatCPU(top.cpu) + (top.cpuBound > 0 ? " / \(formatCPU(top.cpuBound))" : ""),
                      fraction: top.cpuFraction)
            UsageLine(label: String(localized: "Memory"),
                      value: formatBytes(Int64(top.memory)) + (top.memoryBound > 0 ? " / \(formatBytes(Int64(top.memoryBound)))" : ""),
                      fraction: top.memoryFraction)
        }
    }
}

/// "CPU  [bar]  125m / 1", read as one phrase by VoiceOver.
private struct UsageLine: View {
    let label: String
    let value: String
    let fraction: Double?

    var body: some View {
        HStack(spacing: 8) {
            Text(verbatim: label).frame(minWidth: 52, alignment: .leading)
            if let fraction { UsageBar(fraction: fraction) } else { Spacer() }
            Text(verbatim: value).monospacedDigit()
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: "\(label) \(value)"))
    }
}

/// Name, CPU or memory order for a list that has usage.
struct TopSortPicker: View {
    @Binding var sort: TopSort

    var body: some View {
        Picker("Sort by", selection: $sort) {
            Text("Name").tag(TopSort.name)
            Text("CPU").tag(TopSort.cpu)
            Text("Memory").tag(TopSort.memory)
        }
        .pickerStyle(.segmented)
    }
}
