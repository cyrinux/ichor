import SwiftUI
import IchorCore

/// What the schema help was asked for: the field at the caret, or the fields to add there.
struct KubeSchemaHelpRequest: Identifiable {
    enum Mode { case explain, add }

    let mode: Mode
    let cursor: YamlCursor
    /// The caret (UTF-16) the cursor was read at: where a field is added.
    let offset: Int

    var id: String { "\(mode)-\(offset)" }

    /// The path asked about: the field at the caret, or the map a field is added to.
    var path: String { mode == .explain ? cursor.fieldPath : cursor.addPath }
}

/// `kubectl explain` for the field at the caret of the YAML editor, from the cluster's
/// OpenAPI v3 schema: type, description, allowed values and fields; in add mode, the fields
/// to pick from, required ones first.
struct KubeSchemaHelpSheet: View {
    let resource: KubeAPIResource
    let request: KubeSchemaHelpRequest
    /// A field picked to add; `listItem` when it starts a new item of a list.
    let onAdd: (_ child: KubeExplainChild, _ listItem: Bool) -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeExplain> = .loading

    private var title: String {
        let path = request.path.isEmpty ? resource.kind : request.path
        return request.mode == .add ? String(localized: "Add to \(path)") : path
    }

    var body: some View {
        NavigationStack {
            content
                .themedBackground()
                .navigationTitle(Text(verbatim: title))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .task { await load() }
    }

    @ViewBuilder private var content: some View {
        switch state {
        case .loading:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            failure(message)
        case .loaded(let explain, _, _):
            List {
                if request.mode == .explain { explainSections(explain) } else { addSection(explain) }
            }
        }
    }

    @ViewBuilder private func failure(_ message: String) -> some View {
        if message.lowercased().hasPrefix(schemaHelpUnavailablePrefix) {
            let detail = message.dropFirst(schemaHelpUnavailablePrefix.count).drop(while: { $0 == ":" || $0 == " " })
            ContentUnavailableView {
                Label("Schema help unavailable", systemImage: "questionmark.circle")
            } description: {
                Text(verbatim: String(detail))
            }
        } else {
            ContentUnavailableView {
                Label("Request failed", systemImage: "exclamationmark.triangle")
            } description: {
                Text(verbatim: message)
            } actions: {
                Button("Retry") { Task { await load() } }
            }
        }
    }

    @ViewBuilder private func explainSections(_ explain: KubeExplain) -> some View {
        Section {
            HStack(spacing: 8) {
                Text(verbatim: explain.type).font(.callout.monospaced())
                if !explain.format.isEmpty && !explain.type.contains(explain.format) {
                    Text(verbatim: explain.format).font(.caption).foregroundStyle(.secondary)
                }
                if explain.required { requiredTag }
            }
            if !explain.description.isEmpty {
                Text(verbatim: explain.description).font(.callout).textSelection(.enabled)
            }
        }
        if !explain.enumValues.isEmpty {
            Section("Allowed values") {
                Text(verbatim: explain.enumValues.joined(separator: "  ·  ")).font(.caption.monospaced())
            }
        }
        if !explain.children.isEmpty {
            Section("Fields") {
                ForEach(explain.children) { KubeSchemaChildRow(child: $0) }
            }
        }
    }

    @ViewBuilder private func addSection(_ explain: KubeExplain) -> some View {
        if explain.children.isEmpty {
            Text("Nothing to add here: this field holds a value, not other fields.")
                .foregroundStyle(.secondary)
        } else {
            Section {
                ForEach(explain.children.filter(\.required) + explain.children.filter { !$0.required }) { child in
                    Button {
                        onAdd(child, explain.isList && request.cursor.opensBlock)
                        dismiss()
                    } label: {
                        KubeSchemaChildRow(child: child)
                    }
                    .foregroundStyle(.primary)
                    .accessibilityHint(Text("Adds this field at the cursor"))
                }
            } footer: {
                Text("Tap a field to add it at the cursor, required ones first.")
            }
        }
    }

    private var requiredTag: some View {
        Text("required")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.statusWarn)
    }

    private func load() async {
        guard let client = model.client else { return }
        let resource = resource
        let path = request.path
        state = .loading
        state = await .from { try await client.explain(resource, fieldPath: path) }
    }
}

/// One field of the schema: name, type, required, first sentence and allowed values.
private struct KubeSchemaChildRow: View {
    let child: KubeExplainChild

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 8) {
                Text(verbatim: child.name).font(.callout.monospaced().weight(.semibold))
                Text(verbatim: child.type).font(.caption).foregroundStyle(.secondary)
                if child.required {
                    Text("required")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.statusWarn)
                }
            }
            if !child.description.isEmpty {
                Text(verbatim: child.description).font(.caption).foregroundStyle(.secondary)
            }
            if !child.enumValues.isEmpty {
                Text(verbatim: child.enumValues.joined(separator: "  ·  ")).font(.caption.monospaced()).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
