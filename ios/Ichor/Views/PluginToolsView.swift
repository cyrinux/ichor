import SwiftUI
import IchorCore

/// A plugin's tool buttons (docs/plugins.md "Tools"), each with its hint; under them the run in
/// progress, its error, or its result: the title toned by its health, then its cards drawn as
/// the tree's are. One run at a time, over every namespace; the result stays until the screen
/// reads the plugin again (`reads` changes). Same as Android's ui/plugins/PluginTools.kt.
struct PluginToolsView: View {
    let pluginID: String
    let tools: [PluginTool]
    /// Bumped by each read of the plugin: the last result is dropped then.
    let reads: Int
    /// The kind of an object a result row opens, from the ref or the plugin catalog.
    let kindOf: (PluginRef) -> String

    @Environment(AppModel.self) private var model
    @State private var run: PluginToolRun?
    @State private var expanded: Set<String> = []
    @State private var task: Task<Void, Never>?

    // Explicit: the private @State makes the memberwise init private.
    init(pluginID: String, tools: [PluginTool], reads: Int, kindOf: @escaping (PluginRef) -> String) {
        self.pluginID = pluginID
        self.tools = tools
        self.reads = reads
        self.kindOf = kindOf
    }

    var body: some View {
        Group {
            Section {
                ForEach(tools) { tool in
                    VStack(alignment: .leading, spacing: 6) {
                        Button {
                            start(tool)
                        } label: {
                            Text(verbatim: tool.label)
                        }
                        .buttonStyle(.bordered)
                        .disabled(run?.running == true)
                        if !tool.hint.isEmpty {
                            Text(verbatim: tool.hint).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, 4)
                }
                if let run, run.running {
                    HStack(spacing: 10) {
                        ProgressView()
                        Text("Running \(run.label)…").font(.callout).foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                } else if let run, let error = run.error {
                    Label {
                        Text("\(run.label) failed: \(error)")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    .font(.callout)
                    .foregroundStyle(Color.statusBad)
                }
            }
            if let result = run?.result {
                Section {
                    HStack(spacing: 10) {
                        HealthDot(health: result.serviceHealth)
                        Text(verbatim: result.title)
                            .font(.headline)
                            .foregroundStyle(
                                result.serviceHealth.needsAttention ? result.serviceHealth.color : .primary)
                    }
                    if result.shownCards.isEmpty { Text("No result.").note() }
                }
                ForEach(Array(result.shownCards.enumerated()), id: \.offset) { _, card in
                    PluginCardSections(card: card, expanded: $expanded, kindOf: kindOf)
                }
            }
        }
        .onChange(of: reads) { clear() }
    }

    private func start(_ tool: PluginTool) {
        guard let client = model.client, run?.running != true else { return }
        let started = PluginToolRun.started(tool)
        run = started
        expanded = []
        task = Task {
            do {
                let result = try await client.pluginTool(id: pluginID, tool: tool.id)
                guard !Task.isCancelled else { return }
                run = started.finished(result)
            } catch {
                guard !Task.isCancelled else { return }
                run = started.failed(error.localizedDescription)
            }
        }
    }

    /// Drops the result, and stops a run still going.
    private func clear() {
        task?.cancel()
        task = nil
        run = nil
        expanded = []
    }
}
