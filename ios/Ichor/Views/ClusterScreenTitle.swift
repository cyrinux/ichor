import SwiftUI
import IchorCore

/// The title of a screen that changes the cluster (an upgrade, a maintenance, a sync): the
/// title, then the active cluster's colour dot and label under it when several clusters are
/// on the phone, so a screen three levels deep (or one a link opened after switching the
/// cluster) still says which cluster it acts on. The `navigationTitle` stays for the back
/// button; with one cluster the system title shows as before.
private struct ClusterScreenTitle: ViewModifier {
    let title: Text
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        let count = model.summary?.contexts.count ?? 0
        let line = clusterTitleLine(clusterCount: count, activeLabel: model.activeSummary.map { model.labels.of($0) })
        content
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if let line {
                    ToolbarItem(placement: .principal) {
                        VStack(spacing: 1) {
                            title.font(.headline).lineLimit(1)
                            HStack(spacing: 4) {
                                Circle()
                                    .fill(clusterAccent(model.seed(of: model.activeSummary)))
                                    .frame(width: 7, height: 7)
                                    .accessibilityHidden(true)
                                Text(verbatim: line).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }
    }
}

extension View {
    /// `title` with the active cluster under it (see ClusterScreenTitle).
    func clusterScreenTitle(_ title: Text) -> some View {
        modifier(ClusterScreenTitle(title: title))
    }
}
