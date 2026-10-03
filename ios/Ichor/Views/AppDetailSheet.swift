import SwiftUI
import IchorCore

/// One app of the inventory: what needs a look, where it runs, its workloads to restart
/// (os:admin), its images and pods.
struct AppDetailSheet: View {
    let app: InventoryApp
    /// Address → hostname of the overview's nodes.
    let hostnames: [String: String]

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                Section {
                    HStack(spacing: 10) {
                        StatTile(value: "\(app.running)/\(app.containers)", label: String(localized: "Containers"))
                        StatTile(value: "\(app.nodes.count)", label: String(localized: "Nodes"))
                        StatTile(value: formatBytes(app.memory), label: String(localized: "Memory"))
                    }
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
                // Routes and rollout restarts go through the Kubernetes API: only for a role that can reach it.
                if model.allows(.workloads) {
                    AppRoutesSection(app: app)
                    AppWorkloadsSection(app: app)
                }
                if !app.images.isEmpty {
                    Section("Images") {
                        let drifting = app.driftingRepos
                        ForEach(app.images) { ImageLine(image: $0, drifting: drifting.contains($0.repo)) }
                    }
                }
                if !app.pods.isEmpty {
                    Section("Pods") {
                        ForEach(app.pods) { PodLine(pod: $0, hostname: hostnames[$0.node] ?? $0.node) }
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

    private var header: some View {
        VStack(spacing: 10) {
            AppIconView(app: app, size: 76)
            Text(verbatim: app.name)
                .font(.title2.bold())
                .multilineTextAlignment(.center)
            Text(verbatim: ([app.category.label] + app.namespaces).joined(separator: " · "))
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            HStack(spacing: 6) {
                if !app.version.isEmpty { InfoChip(text: app.version, monospaced: true) }
                if app.drift {
                    InfoChip(text: String(localized: "\(app.versionCount) versions running"), color: attentionColor)
                }
                if app.unpinned { InfoChip(text: String(localized: "Unpinned image"), color: attentionColor) }
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
    }
}

private struct StatTile: View {
    let value: String
    let label: String

    var body: some View {
        VStack(spacing: 2) {
            Text(verbatim: value)
                .font(.title3.weight(.semibold))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            Text(verbatim: label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }
}

/// "repo  tag ×2"; an image known only by digest shows a short digest instead.
private struct ImageLine: View {
    let image: InventoryImage
    let drifting: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verbatim: image.repo.isEmpty ? shortDigest(image.digest) : image.repo)
                .font(.callout.monospaced())
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
            Spacer(minLength: 4)
            if !image.tag.isEmpty {
                Text(verbatim: image.tag)
                    .font(.caption.monospaced())
                    .foregroundStyle(drifting ? attentionColor : .secondary)
            }
            Text(verbatim: "×\(image.containers)")
                .font(.caption)
                .foregroundStyle(.secondary)
                .monospacedDigit()
        }
    }
}

/// Status dot, pod name, then "node · containers · memory".
private struct PodLine: View {
    let pod: InventoryPod
    let hostname: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Circle()
                .fill(pod.allRunning ? Color.green : attentionColor)
                .frame(width: 8, height: 8)
                .accessibilityLabel(Text(pod.allRunning ? LocalizedStringKey("Running") : LocalizedStringKey("Not running")))
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: pod.pod)
                    .font(.callout)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(verbatim: [hostname, pod.containers.map(\.name).joined(separator: ", "), formatBytes(pod.memory)]
                    .filter { !$0.isEmpty }
                    .joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
    }
}
