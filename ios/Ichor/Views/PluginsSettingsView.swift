import SwiftUI
import IchorCore

/// Settings → Plugins: the plugins the user imported, each with a switch (off with its reason
/// when this app no longer reads it) and Remove, Import (PluginImportSheet), then the built-in
/// plugins, read-only. Same page as Android's PluginsScreen.kt.
struct PluginsSettingsView: View {
    @State private var imported: [PluginUserItem] = []
    @State private var builtins: [PluginCatalogItem] = []
    @State private var failure: String?
    @State private var importing = false
    @State private var confirmRemove: PluginUserItem?

    var body: some View {
        Section {
            if let failure {
                Text("Plugins: \(failure)").foregroundStyle(.red)
            }
            if imported.isEmpty {
                Text("No imported plugin yet.").foregroundStyle(.secondary)
            }
            ForEach(imported) { item in
                ImportedPluginRow(item: item, enabled: Binding(
                    get: { item.enabled },
                    set: { on in Task { await change { try await TalosClient.setPluginEnabled(id: item.id, enabled: on) } } }
                ))
                .swipeActions {
                    Button("Remove", role: .destructive) { confirmRemove = item }
                }
                .contextMenu {
                    Button(role: .destructive) { confirmRemove = item } label: {
                        Label("Remove", systemImage: "trash")
                    }
                }
            }
            Button { importing = true } label: {
                Label("Import a plugin", systemImage: "square.and.arrow.down")
            }
        } header: {
            Text("Imported")
        } footer: {
            Text("An imported plugin reads only the resources it declares, with your own Kubernetes access, and talks to nothing but the Kubernetes API. The format is described in docs/plugins.md.")
        }
        Section {
            ForEach(builtins, id: \.id) { item in
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: item.name)
                    Text("Reads \(item.resources.map(\.resource).joined(separator: ", "))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Built-in")
        } footer: {
            Text("A plugin shows a family of custom resources as cards. Built-in plugins come with the app; you can import your own YAML file.")
        }
        .task { await load() }
        .sheet(isPresented: $importing, onDismiss: { Task { await load() } }) {
            PluginImportSheet()
        }
        .confirmationDialog(confirmRemove.map { String(localized: "Remove \($0.title)?") } ?? "",
                            isPresented: Binding(get: { confirmRemove != nil }, set: { if !$0 { confirmRemove = nil } }),
                            titleVisibility: .visible, presenting: confirmRemove) { item in
            Button("Remove", role: .destructive) {
                Task { await change { try await TalosClient.removePlugin(id: item.id) } }
            }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("Its file is deleted from this phone and its tab disappears.")
        }
    }

    private func load() async {
        do {
            imported = try await TalosClient.userPlugins().items
            builtins = try await TalosClient.installedPlugins().builtins
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    private func change(_ action: () async throws -> Void) async {
        do {
            try await action()
        } catch {
            failure = error.localizedDescription
        }
        await load()
    }
}

/// An imported plugin: its name and id, what it reads or why it is off, and its switch.
private struct ImportedPluginRow: View {
    let item: PluginUserItem
    @Binding var enabled: Bool

    var body: some View {
        Toggle(isOn: $enabled) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: item.title)
                Text(verbatim: item.id).font(.caption).foregroundStyle(.secondary)
                if item.usable {
                    Text("Reads \(item.readsLine)").font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("Off: \(item.error)").font(.caption).foregroundStyle(.red)
                }
            }
        }
        .disabled(!item.usable)
        .accessibilityHint(Text("Enabled"))
    }
}
