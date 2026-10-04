import SwiftUI
import IchorCore

/// What a sync will do, before it does it: prune (off unless ticked, and then the resources it
/// deletes are listed), dry run, force, apply out-of-sync only, server-side apply and replace,
/// starting from the app's own sync options. resources: a selective sync, empty for all.
struct ArgoSyncSheet: View {
    let app: ArgoApp
    let resources: [ArgoResource]
    let sync: (ArgoSyncOptions) async -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var options: ArgoSyncOptions
    @State private var syncing = false

    init(app: ArgoApp, resources: [ArgoResource], sync: @escaping (ArgoSyncOptions) async -> Void) {
        self.app = app
        self.resources = resources
        self.sync = sync
        _options = State(initialValue: ArgoSyncOptions(defaultsFor: app))
    }

    /// What prune deletes: among the chosen resources for a selective sync.
    private var pruned: [ArgoResource] {
        let ids = Set(resources.map(\.id))
        return app.pruneCandidates.filter { ids.isEmpty || ids.contains($0.id) }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: 12) {
                        AppIconView(app: app.iconApp, size: 40)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: app.name).font(.headline)
                            Text(verbatim: target).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                    if !resources.isEmpty {
                        Label(String(localized: "Only \(resources.count) selected resources"), systemImage: "checklist")
                            .font(.callout)
                    }
                }
                Section {
                    Toggle(isOn: $options.prune) {
                        Text("Prune")
                        Text("Delete the resources that are no longer in Git.")
                    }
                    .tint(.red)
                    if options.prune {
                        if pruned.isEmpty {
                            Text("Nothing would be deleted.").font(.callout).foregroundStyle(.secondary)
                        } else {
                            VStack(alignment: .leading, spacing: 4) {
                                Label(String(localized: "\(pruned.count) resources will be deleted"), systemImage: "trash")
                                    .font(.callout.weight(.semibold))
                                    .foregroundStyle(.statusBad)
                                ForEach(pruned) { resource in
                                    Text(verbatim: "\(resource.kind) \(resource.namespace.isEmpty ? "" : resource.namespace + "/")\(resource.name)")
                                        .font(.caption.monospaced())
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                    Toggle(isOn: $options.dryRun) {
                        Text("Dry run")
                        Text("Check the sync without changing anything.")
                    }
                } footer: {
                    if !pruned.isEmpty && !options.prune {
                        Text("\(pruned.count) resources are no longer in Git and stay as they are.")
                    }
                }
                Section("Options") {
                    Toggle(isOn: $options.applyOutOfSyncOnly) {
                        Text("Apply out-of-sync only")
                        Text("Skip the resources already in sync.")
                    }
                    Toggle(isOn: $options.serverSideApply) {
                        Text("Server-side apply")
                        Text("Let the API server merge the changes.")
                    }
                    Toggle(isOn: $options.force) {
                        Text("Force")
                        Text("Delete and recreate resources that cannot be patched.")
                    }
                    Toggle(isOn: $options.replace) {
                        Text("Replace")
                        Text("Use replace instead of apply.")
                    }
                }
            }
            .navigationTitle(Text("Sync"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    if syncing {
                        ProgressView()
                    } else {
                        Button(options.dryRun ? String(localized: "Dry run") : String(localized: "Sync")) {
                            Task {
                                syncing = true
                                var chosen = options
                                chosen.resources = resources.map(\.ref)
                                await sync(chosen)
                                syncing = false
                                dismiss()
                            }
                        }
                        .fontWeight(.semibold)
                        .tint(options.prune && !pruned.isEmpty ? .red : nil)
                    }
                }
            }
            .interactiveDismissDisabled(syncing)
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    /// The revision a sync goes to: chart@version or repo path@revision.
    private var target: String {
        app.sources.map { source in
            source.chart.isEmpty
                ? "\(source.path.isEmpty ? source.repo : source.path)@\(source.targetRevision)"
                : "\(source.chart)@\(source.targetRevision)"
        }
        .joined(separator: "\n")
    }
}
