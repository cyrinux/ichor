import SwiftUI
import IchorCore

/// `kubectl top` on the Kubernetes screens: CPU and memory in use from metrics-server, as bars
/// on node and pod rows. Without metrics-server the rows simply show no bars.

/// A node's CPU and memory in use against what it can give.
struct NodeUsageView: View {
    let top: KubeTopNode

    var body: some View {
        VStack(spacing: 2) {
            UsageLine(
                label: String(localized: "CPU"),
                value: "\(formatCPU(top.cpu)) / \(formatCPU(top.cpuAllocatable))",
                fraction: top.cpuPercent / 100)
            UsageLine(
                label: String(localized: "Memory"),
                value: "\(formatBytes(Int64(top.memory))) / \(formatBytes(Int64(top.memoryAllocatable)))",
                fraction: top.memoryPercent / 100)
        }
    }
}

/// A pod's CPU and memory in use against its limit, else its request (no bar when neither). With
/// two or more containers, each one's usage against its own bounds follows, in the spec's order,
/// as `kubectl top pod NAME --containers`; a single container would only repeat the pod.
struct PodUsageView: View {
    let top: KubeTopPod

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            UsageLines(
                cpu: top.cpu, cpuBound: top.cpuBound, cpuFraction: top.cpuFraction,
                memory: top.memory, memoryBound: top.memoryBound, memoryFraction: top.memoryFraction)
            if top.containers.count > 1 {
                Text("Containers")
                    .font(.caption.weight(.semibold))
                    .padding(.top, 6)
                ForEach(top.containers) { container in
                    ContainerUsage(container: container)
                }
            }
        }
    }
}

/// One container: its name, then its CPU and memory lines, indented under the pod's.
private struct ContainerUsage: View {
    let container: KubeTopContainer

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: container.name)
                .font(.caption2)
                .lineLimit(1)
                .truncationMode(.tail)
            UsageLines(
                cpu: container.cpu, cpuBound: container.cpuBound, cpuFraction: container.cpuFraction,
                memory: container.memory, memoryBound: container.memoryBound,
                memoryFraction: container.memoryFraction)
        }
        .padding(.leading, 12)
        .padding(.top, 2)
    }
}

/// The CPU and Memory lines of a pod or a container: used, then " / " its limit, else its request.
private struct UsageLines: View {
    let cpu: Double
    let cpuBound: Double
    let cpuFraction: Double?
    let memory: Double
    let memoryBound: Double
    let memoryFraction: Double?

    var body: some View {
        UsageLine(
            label: String(localized: "CPU"),
            value: formatCPU(cpu) + (cpuBound > 0 ? " / \(formatCPU(cpuBound))" : ""),
            fraction: cpuFraction)
        UsageLine(
            label: String(localized: "Memory"),
            value: formatBytes(Int64(memory))
                + (memoryBound > 0 ? " / \(formatBytes(Int64(memoryBound)))" : ""),
            fraction: memoryFraction)
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
