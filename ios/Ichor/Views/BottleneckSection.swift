import SwiftUI
import IchorCore

struct BottleneckSection: View {
    let detail: Bottlenecks
    var body: some View {
        Section("Bottleneck metrics") { BottleneckContent(detail: detail) }
    }
}

struct BottleneckContent: View {
    let detail: Bottlenecks
    private func number(_ n: Double) -> String { String(format: "%.2f", n) }
    private func rate(_ n: Double) -> String { formatBytes(UInt64(max(0, n))) + "/s" }
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if detail.errors["sample"] == nil {
                Text(verbatim: String(format: String(localized: "I/O wait: %@%% · VM steal: %@%%"), number(detail.wait), number(detail.steal)))
            }
            ForEach(detail.disks) { disk in
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: disk.name).font(.headline)
                    Text(verbatim: String(format: String(localized: "Read: %@ · Write: %@ · Active: %@%% · Average I/O: %@ ms"), rate(disk.read), rate(disk.write), number(disk.busy), number(disk.latency)))
                        .font(.caption)
                }
            }
            ForEach(detail.network) { net in
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: net.name).font(.headline)
                    Text(verbatim: String(format: String(localized: "In: %@ · Out: %@ · Errors: %@/s · Drops: %@/s"), rate(net.read), rate(net.write), number(net.errors), number(net.drops)))
                        .font(.caption)
                }
            }
            Text("Rates use consecutive samples. Disk activity and average I/O time are evidence, not proof of saturation.")
                .font(.caption).foregroundStyle(.secondary)
            ForEach(detail.errors.keys.sorted(), id: \.self) { key in
                Text(verbatim: "\(key): \(detail.errors[key] ?? "")").foregroundStyle(.statusBad)
            }
        }
    }
}
