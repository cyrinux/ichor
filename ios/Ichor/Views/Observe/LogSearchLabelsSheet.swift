import SwiftUI
import IchorCore

/// Picks a label, then one of its values, to add to the query.
struct LogSearchLabelsSheet: View {
    let source: PromSource
    let pick: (_ label: String, _ value: String) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var label: String?
    @State private var state: LoadState<[String]> = .loading
    @State private var refreshes = 0

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: { refreshes += 1 }) { items in
                if items.isEmpty {
                    ContentUnavailableView("Nothing seen in the last day", systemImage: "tag")
                } else {
                    List(items, id: \.self) { item in
                        Button {
                            choose(item)
                        } label: {
                            Text(verbatim: item).monoFont()
                        }
                    }
                }
            }
            .navigationTitle(label.map { Text("Values of \($0)") } ?? Text("Pick a label"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                if label != nil {
                    ToolbarItem(placement: .primaryAction) {
                        Button("Labels") { label = nil }
                    }
                }
            }
            .task(id: "\(label ?? "")/\(refreshes)") { await load() }
        }
    }

    private func choose(_ item: String) {
        if let label {
            pick(label, item)
            dismiss()
        } else {
            label = item
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let asked = label
        state = .loading
        do {
            let items = try await client.logsLabels(source, label: asked ?? "")
            guard !Task.isCancelled, asked == label else { return }
            state = .loaded(items, at: Date())
        } catch {
            guard !Task.isCancelled, asked == label else { return }
            state = .failed(error.localizedDescription)
        }
    }
}
