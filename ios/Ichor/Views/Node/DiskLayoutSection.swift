import SwiftUI
import IchorCore

/// Each physical disk of the node: its partition bar, legend and one row per partition
/// (NodeDisks; Talos v1.8+). The core cuts the bar; this view only draws it. Same wording and
/// palette as Android's DiskLayoutSection.
struct DiskLayoutSection: View {
    let state: LoadState<NodeDisks>
    let support: FeatureSupport

    var body: some View {
        Section("Disks") {
            if let notice = support.localizedNotice {
                VersionNoticeRow(text: notice)
            } else {
                SectionStateView(state: state) { list in
                    if !list.supported {
                        VersionNoticeRow(
                            text: FeatureSupport(supported: false, reason: list.reason).localizedNotice
                                ?? String(localized: "Not available on this node's Talos version"))
                    } else if list.disks.isEmpty {
                        Text("No disk reported").font(.footnote).foregroundStyle(.secondary)
                    }
                    ForEach(list.supported ? list.disks : []) { DiskCard(disk: $0) }
                }
            }
        }
    }
}

/// The usual Talos partitions keep their colour on every disk (same palette as Android).
private let wellKnownColors: [String: Int] = [
    "EFI": 0xE0A100, "BIOS": 0x8D6E63, "META": 0x2E9E4F, "STATE": 0x9C4DE0, "EPHEMERAL": 0x3B82F6,
]
private let otherColors: [Int] = [0x14A3A3, 0xD9468F, 0xE36B2C, 0x5C6BC0, 0x7CB342, 0x0097A7]

/// Slices narrower than this share of the bar show no text inside (Android LABEL_MIN_FRACTION).
private let labelMinFraction = 0.18

private func partitionRGB(label: String?, index: Int) -> Int {
    if let label, let rgb = wellKnownColors[label.uppercased()] { return rgb }
    let count = otherColors.count
    return otherColors[((index % count) + count) % count]
}

private func partitionColor(label: String?, index: Int) -> Color {
    Color(uiColor: UIColor(rgb: partitionRGB(label: label, index: index)))
}

/// Black on light slices, white on dark ones.
private func textColor(on rgb: Int) -> Color {
    func channel(_ shift: Int) -> Double {
        let c = Double(rgb >> shift & 0xFF) / 255
        return c <= 0.03928 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
    }
    let luminance = 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
    return luminance > 0.4 ? .black : .white
}

private func partitionLabel(_ partition: DiskPartition) -> String {
    partition.knownLabel ?? String(localized: "Partition \(partition.index)")
}

private struct DiskCard: View {
    let disk: DiskLayout

    var body: some View {
        let slices = barSlices(disk)
        let labels = Dictionary(
            disk.partitions.map { ($0.index, partitionLabel($0)) }, uniquingKeysWith: { first, _ in first })
        VStack(alignment: .leading, spacing: 8) {
            header
            if !slices.isEmpty {
                PartitionBar(slices: slices, labels: labels)
                legend(labels)
            }
            ForEach(disk.partitions) { PartitionRow(partition: $0) }
        }
        .padding(.vertical, 4)
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "internaldrive").foregroundStyle(.secondary).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: disk.name).font(.headline)
                if !disk.devPath.isEmpty {
                    Text(verbatim: disk.devPath).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 4) {
                Text(verbatim: formatBytes(disk.size)).font(.subheadline.monospaced())
                ChipFlow {
                    if disk.systemDisk { StatusPill(label: String(localized: "System disk"), color: .blue) }
                    if !disk.transport.isEmpty { StatusPill(label: disk.transport, color: .secondary) }
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// Dot, label and size per partition, then the unallocated space.
    private func legend(_ labels: [Int: String]) -> some View {
        ChipFlow(spacing: 12) {
            ForEach(disk.partitions) { partition in
                LegendEntry(
                    color: partitionColor(label: partition.knownLabel, index: partition.index),
                    label: labels[partition.index] ?? "", size: partition.size)
            }
            if disk.unallocated > 0 {
                LegendEntry(
                    color: Color.secondary.opacity(0.35), label: String(localized: "Unallocated"),
                    size: disk.unallocated)
            }
        }
    }
}

/// The disk cut into its partitions and free space, by offset, as the core computed them.
private struct PartitionBar: View {
    let slices: [BarSlice]
    let labels: [Int: String]

    var body: some View {
        let unallocated = String(localized: "Unallocated")
        let descriptions = slices.map {
            "\(sliceLabel($0.segment, unallocated: unallocated)), \(formatBytes($0.segment.size))"
        }
        GeometryReader { geo in
            let spacing: CGFloat = 1
            let width = max(geo.size.width - spacing * CGFloat(max(slices.count - 1, 0)), 0)
            HStack(spacing: spacing) {
                ForEach(Array(slices.enumerated()), id: \.offset) { i, slice in
                    sliceView(slice, width: slice.fraction * width, unallocated: unallocated)
                        .accessibilityLabel(Text(verbatim: descriptions[i]))
                }
            }
        }
        .frame(height: 28)
        .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
        // VoiceOver reads each slice ("STATE, 100 MB"); the legend below repeats them as text.
        .accessibilityElement(children: .contain)
    }

    private func sliceView(_ slice: BarSlice, width: CGFloat, unallocated: String) -> some View {
        let segment = slice.segment
        let rgb = partitionRGB(label: labels[segment.index] ?? segment.label, index: segment.index)
        return ZStack {
            if segment.isFree {
                Rectangle().fill(Color.secondary.opacity(0.2))
            } else {
                Rectangle().fill(Color(uiColor: UIColor(rgb: rgb)))
            }
            if slice.fraction >= labelMinFraction, width > 56 {
                Text(verbatim: "\(sliceLabel(segment, unallocated: unallocated)) \(formatBytes(segment.size))")
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(segment.isFree ? Color.secondary : textColor(on: rgb))
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                    .padding(.horizontal, 4)
            }
        }
        .frame(width: width)
    }

    private func sliceLabel(_ segment: DiskSegment, unallocated: String) -> String {
        segment.isFree ? unallocated : labels[segment.index] ?? segment.label
    }
}

private struct LegendEntry: View {
    let color: Color
    let label: String
    let size: UInt64

    var body: some View {
        HStack(spacing: 4) {
            Circle().fill(color).frame(width: 8, height: 8).accessibilityHidden(true)
            Text(verbatim: label).font(.caption.weight(.semibold))
            Text(verbatim: formatBytes(size)).font(.caption).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct PartitionRow: View {
    let partition: DiskPartition

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Circle().fill(partitionColor(label: partition.knownLabel, index: partition.index))
                    .frame(width: 10, height: 10)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 1) {
                    Text(verbatim: partitionLabel(partition)).font(.subheadline.weight(.semibold))
                    if !partition.devPath.isEmpty {
                        Text(verbatim: partition.devPath).font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 8)
                Text(verbatim: formatBytes(partition.size)).font(.subheadline.monospaced())
            }
            Group {
                ChipFlow(spacing: 8) {
                    if !partition.filesystem.isEmpty {
                        Text(verbatim: partition.filesystem).font(.caption.monospaced())
                    }
                    EncryptionChip(partition: partition)
                }
                if !partition.uuid.isEmpty {
                    Text(verbatim: "UUID \(partition.uuid)")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
                let details = [partition.mountedOn, partition.phaseNeedsMention ? partition.phase : ""].filter {
                    !$0.isEmpty
                }
                if !details.isEmpty {
                    Text(verbatim: details.joined(separator: "  ·  ")).font(.caption).foregroundStyle(.secondary)
                }
                if !partition.error.isEmpty {
                    Text(verbatim: partition.error).font(.caption).foregroundStyle(.statusBad)
                }
            }
            .padding(.leading, 20)
        }
    }
}

private struct EncryptionChip: View {
    let partition: DiskPartition

    private var style: (symbol: String, label: String, color: Color) {
        switch partition.encryptionState {
        case .enabled:
            let label = [String(localized: "Encrypted"), partition.encryptionProvider].filter { !$0.isEmpty }.joined(
                separator: " · ")
            return ("lock.fill", label, Color.statusOK)
        case .disabled:
            return ("lock.open", String(localized: "Not encrypted"), Color.statusWarn)
        case .unknown:
            return ("questionmark.circle", String(localized: "Unknown"), Color.secondary)
        }
    }

    var body: some View {
        let (symbol, label, color) = style
        Label {
            Text(verbatim: label)
        } icon: {
            Image(systemName: symbol)
        }
        .font(.caption)
        .foregroundStyle(color)
        .padding(.horizontal, 8)
        .padding(.vertical, 2)
        .background(color.opacity(0.15), in: Capsule())
    }
}
