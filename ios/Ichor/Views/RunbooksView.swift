import SwiftUI
import IchorCore

/// The cluster's runbooks (docs/runbooks.md), from the cluster menu's "Runbooks…": each one's
/// name, description, steps and how many are destructive. A tap opens its plan; Import brings
/// one by paste, file or link (its plan shown first, never run on import); a swipe removes one.
/// Same screen as Android's RunbooksScreen.kt.
struct RunbooksView: View {
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model
    @State private var store = RunbookStore.shared
    @State private var importing = false
    @State private var loading = true
    @State private var failure: String?

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    var body: some View {
        List {
            if let failure {
                Section {
                    Text(verbatim: failure).foregroundStyle(.red)
                }
            }
            let items = store.list(for: fingerprint)
            if items.isEmpty, !loading {
                Section {
                    Text("No runbooks yet. A runbook is an ordered list of this app's actions, written in YAML: import one to plan and run it step by step.")
                        .foregroundStyle(.secondary)
                }
            }
            ForEach(items) { item in
                Button {
                    if item.usable { path.append(.runbookPlan(id: item.id)) }
                } label: {
                    RunbookRow(item: item, running: RunbookJob.shared.run?.id == item.id && RunbookJob.shared.isRunning)
                }
                .foregroundStyle(.primary)
                .swipeActions {
                    Button(role: .destructive) {
                        Task { await remove(item) }
                    } label: {
                        Label("Remove", systemImage: "trash")
                    }
                }
            }
        }
        .overlay { if loading && store.list(for: fingerprint).isEmpty { ProgressView() } }
        .navigationTitle("Runbooks")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { importing = true } label: {
                    Label("Import runbook", systemImage: "square.and.arrow.down")
                }
            }
        }
        .sheet(isPresented: $importing) {
            RunbookImportSheet(fingerprint: fingerprint)
        }
        .refreshable { await load() }
        .task(id: fingerprint) { await load() }
    }

    private func load() async {
        defer { loading = false }
        do {
            try await store.reload(fingerprint: fingerprint)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    private func remove(_ item: RunbookItem) async {
        do {
            try await store.remove(item.id, fingerprint: fingerprint)
        } catch {
            failure = error.localizedDescription
        }
    }
}

private struct RunbookRow: View {
    let item: RunbookItem
    let running: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: item.name).font(.headline)
                if running {
                    Text("Running").font(.caption).foregroundStyle(.blue)
                }
            }
            if !item.description.isEmpty {
                Text(verbatim: item.description).font(.subheadline).foregroundStyle(.secondary)
            }
            if item.usable {
                HStack(spacing: 12) {
                    Text("\(String(item.steps)) steps")
                    if item.destructive > 0 {
                        Label {
                            Text("\(String(item.destructive)) destructive")
                        } icon: {
                            Image(systemName: "exclamationmark.triangle")
                        }
                        .foregroundStyle(.orange)
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            } else {
                Text(verbatim: item.problem).font(.caption).foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
