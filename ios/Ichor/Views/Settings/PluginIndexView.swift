import SwiftUI
import IchorCore

/// Settings → Plugins → Browse the catalog: the plugin files of the index at `indexURL` ("" for the
/// project's). Import downloads one, refused unless it has the digest the index published, has the
/// core check it, and opens the same consent step as any other import: only its Enable stores it.
/// Same screen as Android's PluginIndexScreen.kt.
struct PluginIndexView: View {
    /// A file downloaded, verified and checked, waiting for the consent step.
    private struct Pick: Identifiable {
        let yaml: String
        let item: PluginUserItem
        var id: String { item.id }
    }

    let indexURL: String

    @Environment(\.openURL) private var openURL
    @State private var index: PluginIndex?
    @State private var failure: String?
    @State private var importingID: String?
    @State private var pick: Pick?

    var body: some View {
        Form {
            Section {
                Text("A plugin from the catalog must match the digest the index published, then you see what it reads and can change before enabling it.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if let failure {
                    LoadFailureRow(message: failure, retry: { await load() })
                }
                if let index {
                    Text("From \(index.url)").font(.footnote).foregroundStyle(.secondary)
                    if index.plugins.isEmpty {
                        Text("This index lists no plugin yet.").foregroundStyle(.secondary)
                    }
                } else if failure == nil {
                    HStack {
                        ProgressView()
                        Text("Loading the catalog…").foregroundStyle(.secondary)
                    }
                }
            }
            ForEach(index?.plugins ?? []) { item in
                Section {
                    row(item)
                }
            }
        }
        .navigationTitle(Text("Browse the catalog"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: indexURL) { await load() }
        .sheet(item: $pick, onDismiss: { Task { await load() } }) { pick in
            PluginConsentSheet(yaml: pick.yaml, item: pick.item)
        }
    }

    private func row(_ item: PluginIndexItem) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                AppIconView(app: item.iconApp, size: 40)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: item.title)
                    if !item.description.isEmpty {
                        Text(verbatim: item.description).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer()
                trailing(item)
            }
            if !item.resources.isEmpty {
                Text(verbatim: item.resources.joined(separator: ", "))
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
            if !item.maintainer.isEmpty {
                Text("Maintained by \(item.maintainer)").font(.caption).foregroundStyle(.secondary)
            }
            if !item.website.isEmpty, let url = URL(string: item.website) {
                Button(item.website) { openURL(url) }
                    .font(.caption)
                    .buttonStyle(.borderless)
            }
        }
    }

    @ViewBuilder private func trailing(_ item: PluginIndexItem) -> some View {
        if item.builtin {
            Text("Built-in").font(.caption).foregroundStyle(.secondary)
        } else if item.installed {
            Text("Installed").font(.caption).foregroundStyle(.secondary)
        } else if importingID == item.id {
            ProgressView()
        } else {
            Button("Import") { Task { await importPlugin(item) } }
                .buttonStyle(.bordered)
                .disabled(importingID != nil)
        }
    }

    private func load() async {
        do {
            index = try await TalosClient.pluginIndex(url: indexURL)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    private func importPlugin(_ item: PluginIndexItem) async {
        importingID = item.id
        failure = nil
        defer { importingID = nil }
        do {
            let yaml = try await TalosClient.downloadPluginFromIndex(url: item.url, sha256: item.sha256)
            pick = Pick(yaml: yaml, item: try await TalosClient.checkPlugin(yaml: yaml))
        } catch {
            failure = error.localizedDescription
        }
    }
}
