import SwiftUI
import IchorCore

/// "Since you last looked" on a cluster's home: what the history ring saw happen after the user
/// last looked at it. Owned by the home screen, loaded by its task (see watch), so the section
/// can stay out of the list until there is something to say.
@Observable
@MainActor
final class HistoryLook {
    private(set) var summary: HistorySinceSummary?
    private var cluster = ""

    /// Loads what happened on `cluster` since the last look, then marks the home looked at once
    /// it stayed on screen a few seconds (the banner stays until the next visit or a dismiss).
    /// The first visit only starts counting: nothing to compare with yet.
    func watch(cluster: String) async {
        self.cluster = cluster
        summary = nil
        guard !cluster.isEmpty else { return }
        if let since = HistoryLastLooked.since(stored: HistoryStore.lastLooked(cluster: cluster)) {
            let loaded = await HistoryStore.since(cluster: cluster, lastLooked: since)
            guard !Task.isCancelled, self.cluster == cluster else { return }
            summary = loaded.flatMap { $0.hasNews ? $0 : nil }
        }
        try? await Task.sleep(for: .seconds(HistoryLastLooked.dwellSeconds))
        guard !Task.isCancelled, self.cluster == cluster else { return }
        HistoryStore.setLastLooked(cluster: cluster)
    }

    func dismiss() {
        HistoryStore.setLastLooked(cluster: cluster)
        withAnimation { summary = nil }
    }
}

/// The banner's section: nodes that went down and came back or are still down, alerts that
/// fired and resolved or are still open, upgrades. Nothing when there is no news.
struct HistorySinceSection: View {
    let look: HistoryLook

    /// Rows past this many are counted, not listed.
    private let shown = 8

    var body: some View {
        if let summary = look.summary {
            Section {
                let rows = entries(summary)
                ForEach(rows.prefix(shown)) { $0 }
                if rows.count > shown {
                    Text("\(rows.count - shown) more").font(.footnote).foregroundStyle(.secondary)
                }
                Button("Dismiss") { look.dismiss() }
            } header: {
                Text("Since you last looked")
            }
        }
    }

    private func entries(_ summary: HistorySinceSummary) -> [HistorySinceRow] {
        var out: [HistorySinceRow] = []
        for node in summary.nodesDown {
            let health: NodeHealth = node.state == "unreachable" ? .unreachable : .notReady
            let title = node.downAt > summary.from ? String(localized: "\(node.label) went down") : String(localized: "\(node.label) is still down")
            out.append(HistorySinceRow(id: "down \(node.node)", title: title, icon: "xmark.circle.fill", tint: health.color, at: node.downAt))
        }
        for alert in summary.alertsOpen {
            let title = summary.isNew(alert) ? String(localized: "\(alert.label) fired") : String(localized: "\(alert.label) is still open")
            let tint: Color = alert.severity == dataCritical ? .statusBad : .statusWarn
            out.append(HistorySinceRow(id: "open \(alert.track) \(alert.key)", title: title, icon: "exclamationmark.triangle.fill",
                                       tint: tint, at: alert.openedAt))
        }
        for node in summary.nodesRecovered {
            out.append(HistorySinceRow(id: "back \(node.node) \(node.downAt)", title: String(localized: "\(node.label) was down and is back"),
                                       icon: "arrow.uturn.up.circle.fill", tint: .statusOK, at: node.upAt ?? node.downAt))
        }
        for alert in summary.alertsResolved {
            out.append(HistorySinceRow(id: "resolved \(alert.track) \(alert.key) \(alert.openedAt)", title: String(localized: "\(alert.label) resolved"),
                                       icon: "checkmark.circle.fill", tint: .statusOK, at: alert.closedAt ?? alert.openedAt))
        }
        for upgrade in summary.upgrades {
            out.append(HistorySinceRow(id: "upgrade \(upgrade.node) \(upgrade.at)", title: String(localized: "\(upgrade.label) upgraded"),
                                       detail: "\(upgrade.from) → \(upgrade.to)", icon: "arrow.up.circle.fill", tint: .blue, at: upgrade.at))
        }
        return out
    }
}

/// One line of the banner: what happened, and when.
private struct HistorySinceRow: View, Identifiable {
    let id: String
    let title: String
    var detail = ""
    let icon: String
    let tint: Color
    /// Epoch ms.
    let at: Int64

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title)
                    if !detail.isEmpty {
                        Text(verbatim: detail).font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                }
            } icon: {
                Image(systemName: icon).foregroundStyle(tint)
            }
            Spacer()
            Text(verbatim: Date(timeIntervalSince1970: Double(at) / 1000).formatted(.relative(presentation: .named)))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

extension View {
    /// Loads the "since you last looked" banner of `cluster` while this screen shows; `id`
    /// changes reload it (another cluster, the screenshot mode toggled).
    func watchesHistory(_ look: HistoryLook, cluster: String, id: String) -> some View {
        task(id: id) { await look.watch(cluster: cluster) }
    }
}

/// A node's uptime over the last 7 or 30 days: a strip of ready, not ready, unreachable and
/// no data (the phone did not check), with the uptime %.
struct NodeUptimeStrip: View {
    let uptime: HistoryNodeUptime
    let from: Int64
    let to: Int64
    @Binding var period: HistoryPeriod

    var body: some View {
        let percent = historyUptimeText(uptime.uptimePercent)
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Uptime").font(.subheadline)
                if !percent.isEmpty {
                    Text(verbatim: percent).font(.subheadline.weight(.semibold)).monospacedDigit()
                }
                Spacer()
                Picker("Period", selection: $period) {
                    Text("7 days").tag(HistoryPeriod.week)
                    Text("30 days").tag(HistoryPeriod.month)
                }
                .pickerStyle(.segmented)
                .fixedSize()
            }
            HistoryStrip(segments: historyStrip(uptime.intervals, from: from, to: to))
                .accessibilityElement()
                .accessibilityLabel(Text("Uptime") + Text(verbatim: " \(percent)"))
        }
    }
}

/// The strip itself: one coloured run per segment.
private struct HistoryStrip: View {
    let segments: [HistoryStripSegment]

    var body: some View {
        Canvas { context, size in
            for segment in segments {
                let rect = CGRect(x: size.width * segment.start, y: 0,
                                  width: max(1, size.width * (segment.end - segment.start)), height: size.height)
                context.fill(Path(rect), with: .color(color(segment.state)))
            }
        }
        .frame(height: 10)
        .clipShape(RoundedRectangle(cornerRadius: 3))
    }

    private func color(_ state: HistoryStripState) -> Color {
        switch state {
        case .ready: NodeHealth.ready.color
        case .notReady: NodeHealth.notReady.color
        case .unreachable: NodeHealth.unreachable.color
        case .noData: Color.secondary.opacity(0.2)
        }
    }
}

/// A % used series (memory, a volume's fill) as a small line on a fixed 0–100 % scale over the
/// window [from, to]; coloured by the last value like UsageBar. Nothing under two points.
struct HistorySparkline: View {
    let points: [HistoryPoint]
    let from: Int64
    let to: Int64
    let label: Text

    var body: some View {
        let last = points.last?.value ?? 0
        let color: Color = last >= 90 ? .red : last >= 75 ? .orange : .green
        Canvas { context, size in
            guard points.count >= 2, to > from else { return }
            let stroke = 1.5
            func x(_ t: Int64) -> Double { size.width * Double(min(max(t, from), to) - from) / Double(to - from) }
            func y(_ v: Double) -> Double { stroke / 2 + (size.height - stroke) * (1 - min(max(v, 0), 100) / 100) }
            var line = Path()
            for (i, point) in points.enumerated() {
                let p = CGPoint(x: x(point.at), y: y(point.value))
                if i == 0 { line.move(to: p) } else { line.addLine(to: p) }
            }
            context.stroke(line, with: .color(color), style: StrokeStyle(lineWidth: stroke, lineCap: .round, lineJoin: .round))
        }
        .frame(height: 20)
        .accessibilityElement()
        .accessibilityLabel(label)
    }
}
