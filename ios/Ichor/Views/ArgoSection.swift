import SwiftUI
import IchorCore

/// The overview's GitOps row: the Argo CD apps' health as a bar, the counts, the syncs running
/// with their wave, and at most two problem apps with their likely cause; opens the Argo CD
/// screen. Only shown when the inventory has Argo CD (and the role may use the Kubernetes API);
/// one calm line when everything is Synced and Healthy.
struct ArgoSection: View {
    let state: LoadState<ArgoStatus>
    /// Argo CD's own inventory app, for its icon.
    let app: InventoryApp?
    let downNodes: Set<String>

    var body: some View {
        GitOpsOverviewSection(title: "GitOps", route: .argoCD(downNodes: downNodes), app: app ?? argoCDTile, state: state,
                              installed: { $0.installed && !$0.apps.isEmpty }) {
            ArgoHealthBar(counts: [(health: .healthy, count: 1)])
            Text(verbatim: "GitOps apps healthy").font(.caption)
        } content: { content($0) }
    }

    private func content(_ status: ArgoStatus) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            GitOpsSectionHeader(app: app ?? argoCDTile, title: "GitOps", count: status.apps.count)
            ArgoHealthBar(counts: status.healthCounts)
            if let next = status.activeFreezes.first {
                Label(status.activeFreezes.count == 1
                      ? String(localized: "1 freeze · ends \(freezeClock(next.window.endsAt))")
                      : String(localized: "\(status.activeFreezes.count) freezes · next ends \(freezeClock(next.window.endsAt))"),
                      systemImage: "snowflake")
                    .font(.caption)
                    .foregroundStyle(.blue)
            }
            if status.allCalm && status.running.isEmpty {
                Text("All apps synced and healthy").font(.caption).foregroundStyle(.secondary)
            } else {
                counts(status)
                ForEach(status.running.prefix(3)) { running($0) }
                ForEach(status.problems.filter { !$0.isRunning }.prefix(2)) { problem($0) }
            }
        }
        .padding(.vertical, 4)
    }

    private func counts(_ status: ArgoStatus) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 12) { countItems(status) }
            VStack(alignment: .leading, spacing: 2) { countItems(status) }
        }
        .font(.caption)
        .monospacedDigit()
    }

    @ViewBuilder private func countItems(_ status: ArgoStatus) -> some View {
        ForEach(status.healthCounts.indices, id: \.self) { i in
            let part = status.healthCounts[i]
            HStack(spacing: 4) {
                Circle().fill(part.health.color).frame(width: 7, height: 7)
                    .accessibilityHidden(true)
                Text(verbatim: "\(part.count) \(part.health.label.lowercased())")
            }
        }
        let drifting = status.count(.outOfSync)
        if drifting > 0 {
            HStack(spacing: 4) {
                Image(systemName: ArgoSyncState.outOfSync.symbol).foregroundStyle(attentionColor)
                Text(verbatim: "\(drifting) \(ArgoSyncState.outOfSync.label.lowercased())")
            }
        }
    }

    private func running(_ app: ArgoApp) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "arrow.triangle.2.circlepath")
                .foregroundStyle(.blue)
                .symbolEffect(.pulse)
            Text(verbatim: app.name).font(.caption.weight(.semibold)).lineLimit(1)
            if let op = app.operation {
                Text(verbatim: argoProgressText(op)).font(.caption).foregroundStyle(.secondary).monospacedDigit()
                Spacer(minLength: 4)
                if op.total > 0 {
                    ProgressView(value: Double(op.done), total: Double(op.total))
                        .frame(maxWidth: 70)
                        .tint(.blue)
                }
            }
        }
    }

    private func problem(_ app: ArgoApp) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(app.level.color).font(.caption)
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: "\(app.name) · \(app.lastSyncFailed ? String(localized: "sync failed") : app.health.broken ? app.health.label : app.sync.label)")
                    .font(.caption.weight(.semibold))
                    .lineLimit(1)
                if let cause = app.likelyCause(downNodes: downNodes) {
                    Text(verbatim: cause.text)
                        .font(.caption)
                        .foregroundStyle(cause.pointsAtPod ? Color.red : Color.secondary)
                        .lineLimit(2)
                }
            }
        }
    }
}
