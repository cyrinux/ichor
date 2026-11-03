import SwiftUI
import IchorCore

/// The images report of the inventory the Apps screen holds, as a sheet: unpinned images,
/// repositories on several tags, registries, and (Talos) the images a node keeps that none of
/// its containers runs. An app row closes the sheet for the app's own (`onApp` with its id); an
/// unused image opens its node's Images screen.
struct ImageReportView: View {
    let inventory: ClusterInventory
    /// Address → hostname of the overview's nodes.
    let hostnames: [String: String]
    let onApp: (String) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<ImageReport> = .loading

    /// A kubeconfig cluster has no node image list: the last part says so.
    private var talos: Bool { !(model.client?.kubeOnly ?? true) }

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: load) { report in
                List {
                    summary(report)
                    unpinnedSection(report)
                    multiTagSection(report)
                    registriesSection(report)
                    unusedSection(report)
                }
                .refreshable { await load() }
            }
            .navigationTitle("Images report")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .task { await load() }
    }

    private func summary(_ report: ImageReport) -> some View {
        Section {
            HStack(spacing: 8) {
                CountTile(count: report.summary.unpinned, label: String(localized: "Unpinned"))
                CountTile(count: report.summary.multiTag, label: String(localized: "Several tags"))
                CountTile(count: report.summary.dockerHub, label: String(localized: "Docker Hub"))
                if talos {
                    CountTile(count: report.summary.unused, label: String(localized: "Unused on nodes"))
                }
            }
        }
    }

    private func unpinnedSection(_ report: ImageReport) -> some View {
        Section {
            if report.unpinned.isEmpty { nothing }
            ForEach(report.unpinned) { row in
                AppsRow(label: row.image, apps: row.apps, hosts: row.nodes.map(host), onApp: open)
            }
        } header: {
            Text("Unpinned")
        } footer: {
            Text("Run as :latest or with no tag: a restart can pull another version.")
        }
    }

    private func multiTagSection(_ report: ImageReport) -> some View {
        Section {
            if report.multiTag.isEmpty { nothing }
            ForEach(report.multiTag) { repo in
                Text(verbatim: repo.repository)
                    .font(.callout.monospaced())
                ForEach(repo.tags) { tag in
                    AppsRow(label: tag.tag, apps: tag.apps, hosts: tag.nodes.map(host), onApp: open)
                        .padding(.leading, 12)
                }
            }
        } header: {
            Text("Several tags")
        } footer: {
            Text("One repository runs with more than one tag at once.")
        }
    }

    private func registriesSection(_ report: ImageReport) -> some View {
        Section {
            ForEach(report.registries) { registry in
                RegistryRow(registry: registry)
            }
        } header: {
            Text("Registries")
        } footer: {
            if report.summary.dockerHub > 0 {
                Text("Docker Hub limits anonymous pulls: \(String(report.summary.dockerHub)) images come from it.")
            }
        }
    }

    private func unusedSection(_ report: ImageReport) -> some View {
        Section {
            if !talos {
                Text("Only a Talos cluster lists the images kept on its nodes.")
                    .foregroundStyle(.secondary)
            } else if report.unusedOnNode.isEmpty {
                nothing
            }
            ForEach(report.unusedOnNode) { row in
                NavigationLink {
                    ImagesView(node: row.node, hostname: host(row.node))
                } label: {
                    UnusedRow(row: row, host: host(row.node))
                }
            }
        } header: {
            Text("Unused on nodes")
        } footer: {
            if talos {
                let size = ByteCountFormatter.string(fromByteCount: report.unusedBytes, countStyle: .file)
                Text("Kept on a node's disk, run by no container there: \(size) in all.")
            }
        }
    }

    private var nothing: some View {
        Text("Nothing to report.").foregroundStyle(.secondary)
    }

    private func host(_ node: String) -> String { hostnames[node] ?? node }

    private func open(_ id: String) {
        onApp(id)
        dismiss()
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<ImageReport> = await .from { try await client.imageReport(inventory: inventory) }
        state = state.refreshed(with: loaded)
    }
}

/// A count over its label, like the app sheet's stat tiles.
private struct CountTile: View {
    let count: Int
    let label: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: String(count))
                .font(.headline)
                .monospacedDigit()
            Text(verbatim: label)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .lineLimit(2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// An image or a tag, then "app, app · node, node"; a tap opens the first app's sheet.
private struct AppsRow: View {
    let label: String
    let apps: [ReportAppRef]
    let hosts: [String]
    let onApp: (String) -> Void

    var body: some View {
        Button {
            if let first = apps.first { onApp(first.id) }
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: label)
                    .font(.footnote.monospaced())
                    .lineLimit(2)
                Text(verbatim: detail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .buttonStyle(.plain)
        .disabled(apps.isEmpty)
    }

    private var detail: String {
        [apps.map(\.name).joined(separator: ", "), hosts.joined(separator: ", ")]
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
    }
}

private struct RegistryRow: View {
    let registry: ReportRegistry

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: registry.host)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                let images = String(localized: "\(String(registry.images)) images")
                let apps = String(localized: "\(String(registry.apps)) apps")
                Text(verbatim: "\(images) · \(apps)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if registry.dockerHub {
                Text("Docker Hub")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
    }
}

private struct UnusedRow: View {
    let row: ReportUnused
    let host: String

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: row.image)
                    .font(.footnote.monospaced())
                    .lineLimit(2)
                Text(verbatim: host)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if row.sizeBytes > 0 {
                Text(verbatim: ByteCountFormatter.string(fromByteCount: row.sizeBytes, countStyle: .file))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }
}
