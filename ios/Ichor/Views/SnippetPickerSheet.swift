import SwiftUI
import IchorCore

/// The ready-made config changes: pick one, fill its parameters, and `onApply` adds it to the
/// draft (the change then shows in the tree and the review, like any edit).
struct SnippetPickerSheet: View {
    let onApply: (_ id: String, _ params: [String: String]) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<ConfigSnippetList> = .loading

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: load) { list in
                List {
                    ForEach(list.byGroup, id: \.group) { entry in
                        Section(snippetGroupTitle(entry.group)) {
                            ForEach(entry.snippets) { snippet in
                                NavigationLink {
                                    SnippetForm(snippet: snippet) { params in
                                        onApply(snippet.id, params)
                                        dismiss()
                                    }
                                } label: {
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(snippetTitle(snippet.id))
                                        Text(snippetDescription(snippet.id)).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Snippets")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await LoadState.from { try await client.configSnippets() }
    }
}

/// The parameters of one snippet.
private struct SnippetForm: View {
    let snippet: ConfigSnippet
    let onApply: ([String: String]) -> Void

    @State private var values: [String: String] = [:]

    private var complete: Bool {
        snippet.params.allSatisfy { $0.type == "bool" || !(values[$0.name] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    }

    var body: some View {
        Form {
            Section {
                Text(snippetDescription(snippet.id)).font(.footnote).foregroundStyle(.secondary)
                if snippet.staged {
                    Text("Takes effect at the next boot: apply it staged or with a reboot.").font(.footnote).foregroundStyle(.secondary)
                }
            }
            Section {
                ForEach(snippet.params, id: \.name) { param in field(param) }
            }
        }
        .navigationTitle(snippetTitle(snippet.id))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Add to the draft") {
                    var params = values
                    for p in snippet.params where p.type == "bool" && params[p.name] == nil { params[p.name] = "false" }
                    onApply(params)
                }
                .disabled(!complete)
            }
        }
    }

    @ViewBuilder
    private func field(_ param: SnippetParam) -> some View {
        let binding = Binding(get: { values[param.name] ?? "" }, set: { values[param.name] = $0 })
        switch param.type {
        case "bool":
            Toggle(snippetParamTitle(param.name), isOn: Binding(get: { binding.wrappedValue == "true" }, set: { binding.wrappedValue = $0 ? "true" : "false" }))
        case "list":
            VStack(alignment: .leading, spacing: 4) {
                Text(snippetParamTitle(param.name)).font(.caption).foregroundStyle(.secondary)
                TextField(text: binding, prompt: Text(verbatim: param.hint), axis: .vertical) { Text(snippetParamTitle(param.name)) }
                    .lineLimit(3...8)
                    .font(.body.monospaced())
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
        default:
            TextField(text: binding, prompt: Text(verbatim: param.hint)) { Text(snippetParamTitle(param.name)) }
                .font(.body.monospaced())
                .keyboardType(param.type == "int" ? .numberPad : .asciiCapable)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
    }
}

private func snippetGroupTitle(_ group: String) -> String {
    switch group {
    case "network": String(localized: "Network")
    case "kernel": String(localized: "Kernel")
    case "kubernetes": String(localized: "Kubernetes")
    default: String(localized: "Cluster")
    }
}

private func snippetTitle(_ id: String) -> String {
    switch id {
    case "ntp": String(localized: "NTP servers")
    case "dns": String(localized: "DNS servers")
    case "mtu": String(localized: "Interface MTU")
    case "sysctl": String(localized: "Sysctl")
    case "kernel_args": String(localized: "Kernel arguments")
    case "kubelet_arg": String(localized: "Kubelet argument")
    case "apiserver_arg": String(localized: "API server argument")
    case "node_label": String(localized: "Node label")
    case "registry_mirror": String(localized: "Registry mirror")
    default: String(localized: "KubeSpan")
    }
}

private func snippetDescription(_ id: String) -> String {
    switch id {
    case "ntp": String(localized: "Replace the time servers.")
    case "dns": String(localized: "Replace the nameservers.")
    case "mtu": String(localized: "Set the MTU of one interface.")
    case "sysctl": String(localized: "Set one kernel parameter.")
    case "kernel_args": String(localized: "Replace the extra kernel arguments of the installation.")
    case "kubelet_arg": String(localized: "Set one extra argument of the kubelet.")
    case "apiserver_arg": String(localized: "Set one extra argument of the API server (control planes only).")
    case "node_label": String(localized: "Set one label of the Kubernetes node.")
    case "registry_mirror": String(localized: "Pull a registry's images through a mirror.")
    default: String(localized: "Turn the WireGuard mesh between the nodes on or off.")
    }
}

private func snippetParamTitle(_ name: String) -> String {
    switch name {
    case "servers": String(localized: "Servers (one per line)")
    case "nameservers": String(localized: "Nameservers (one per line)")
    case "interface": String(localized: "Interface")
    case "mtu": String(localized: "MTU")
    case "key": String(localized: "Name")
    case "args": String(localized: "Arguments (one per line)")
    case "host": String(localized: "Registry")
    case "endpoint": String(localized: "Mirror URL")
    case "enabled": String(localized: "Enabled")
    default: String(localized: "Value")
    }
}
