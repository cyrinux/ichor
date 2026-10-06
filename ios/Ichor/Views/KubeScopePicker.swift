import SwiftUI
import IchorCore

/// The namespace the Kubernetes lists show (`scope`), the cluster's namespaces when they could
/// be listed (nil until then, or when that failed), and how to pick another one. Not `ready`:
/// there is no scope to list yet, the user must type a namespace (see defaultScope).
struct KubeScopeControl {
    let scope: KubeScope
    let namespaces: KubeNamespaces?
    var ready = true
    let onScope: (KubeScope) -> Void
}

/// The namespace picker shared by the Workloads, Pods and CronJobs lists: chips ("All
/// namespaces" first), a searchable sheet past scopeChipsMax namespaces, or a field to type one
/// when the credentials cannot list them. `loaded`: namespaces of the rows on screen, offered
/// while the cluster's are unknown.
struct KubeScopeBar: View {
    let control: KubeScopeControl
    let loaded: [String]

    @State private var sheet = false

    var body: some View {
        let choices = scopeChoices(listed: control.namespaces, loaded: loaded, scope: control.scope)
        Group {
            if control.namespaces?.forbidden == true {
                TypedNamespaceField(control: control)
            } else if choices.count > scopeChipsMax {
                Button { sheet = true } label: {
                    Label {
                        if let namespace = control.scope.namespace {
                            Text(verbatim: namespace).font(.callout.monospaced())
                        } else {
                            Text("All namespaces")
                        }
                    } icon: {
                        Image(systemName: "line.3.horizontal.decrease.circle")
                    }
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .controlSize(.small)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal)
            } else {
                chips(choices)
            }
        }
        .padding(.vertical, 6)
        .sheet(isPresented: $sheet) {
            NamespaceSheet(choices: choices, selected: control.scope.namespace) { picked in
                sheet = false
                control.onScope(KubeScope(namespace: picked, chosen: true))
            }
        }
    }

    private func chips(_ choices: [String]) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                chip(selected: control.scope.namespace == nil, namespace: nil) { Text("All namespaces") }
                ForEach(choices, id: \.self) { namespace in
                    chip(selected: control.scope.namespace == namespace, namespace: namespace) {
                        Text(verbatim: namespace).font(.callout.monospaced())
                    }
                }
            }
            .padding(.horizontal)
        }
    }

    private func chip<L: View>(selected: Bool, namespace: String?, @ViewBuilder label: () -> L) -> some View {
        Button { control.onScope(KubeScope(namespace: namespace, chosen: true)) } label: { label() }
            .buttonStyle(.bordered)
            .buttonBorderShape(.capsule)
            .controlSize(.small)
            .tint(selected ? Color.accentColor : Color.secondary)
            .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Every namespace, searchable; `onPick` nil for every namespace.
private struct NamespaceSheet: View {
    let choices: [String]
    let selected: String?
    let onPick: (String?) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var search = ""

    var body: some View {
        NavigationStack {
            List {
                if search.trimmingCharacters(in: .whitespaces).isEmpty {
                    row(Text("All namespaces"), selected: selected == nil) { onPick(nil) }
                }
                ForEach(matchingNamespaces(choices, query: search), id: \.self) { namespace in
                    row(Text(verbatim: namespace).font(.body.monospaced()), selected: selected == namespace) { onPick(namespace) }
                }
            }
            .searchable(text: $search, placement: .navigationBarDrawer(displayMode: .always), prompt: Text("Search namespaces"))
            .navigationTitle(Text("Namespace"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func row(_ label: Text, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack {
                label.foregroundStyle(.primary)
                Spacer()
                if selected { Image(systemName: "checkmark").foregroundStyle(.tint) }
            }
            .contentShape(Rectangle())
        }
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// The credentials cannot list namespaces (L6): the user types the one to list, remembered.
private struct TypedNamespaceField: View {
    let control: KubeScopeControl

    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                TextField(text: $text, prompt: Text("Namespace")) { Text("Namespace") }
                    .font(.body.monospaced())
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.done)
                    .onSubmit(apply)
                    .textFieldStyle(.roundedBorder)
                Button(action: apply) { Image(systemName: "checkmark") }
                    .buttonStyle(.bordered)
                    .accessibilityLabel(Text("Show this namespace"))
            }
            Text("These credentials cannot list namespaces: type the one to show.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal)
        .task(id: control.scope) { text = control.scope.namespace ?? "" }
    }

    private func apply() {
        let typed = text.trimmingCharacters(in: .whitespaces)
        control.onScope(KubeScope(namespace: typed.nonEmpty, chosen: true))
    }
}

/// A thin bar while pages load ("1,500 / ~4,000"); nothing for a list that came in one page,
/// so a small cluster looks as before.
struct PagedProgressBar<T>: View {
    let progress: PagedLoad<T>?

    var body: some View {
        if let progress, !progress.done {
            let loaded = Int64(progress.items.count)
            VStack(alignment: .leading, spacing: 2) {
                if let total = progress.estimatedTotal, total > 0 {
                    ProgressView(value: min(Double(loaded) / Double(total), 1))
                    Text("\(loaded.formatted()) / ~\(total.formatted())")
                } else {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.mini)
                        Text("\(loaded.formatted()) loaded")
                    }
                }
            }
            .font(.caption2)
            .foregroundStyle(.secondary)
            .monospacedDigit()
            .padding(.horizontal)
            .padding(.vertical, 4)
        }
    }
}

/// What an incomplete list means (L8): past the cap, how much is shown and a hint to pick a
/// namespace, with buttons to load the next page or the rest (while searching, scrolling
/// does not load more); and, while searching or capped, that sorting and search only cover
/// the loaded rows.
struct IncompleteNotice<T>: View {
    let load: PagedLoad<T>
    let searching: Bool
    let loadingMore: Bool
    let onLoadMore: () -> Void
    let onLoadAll: () -> Void

    var body: some View {
        let capped = load.hasMore && load.capped
        if !load.done && (capped || searching) {
            VStack(alignment: .leading, spacing: 4) {
                if capped {
                    let shown = Int64(load.items.count).formatted()
                    if let total = load.estimatedTotal {
                        Text("Showing \(shown) of ~\(total.formatted()) — pick a namespace")
                    } else {
                        Text("Showing the first \(shown) — pick a namespace")
                    }
                }
                Text("Sorting and search only cover the loaded rows.")
                    .foregroundStyle(.secondary)
                if capped {
                    HStack(spacing: 12) {
                        Button("Load next page", action: onLoadMore)
                        Button("Load all", action: onLoadAll)
                        if loadingMore { ProgressView().controlSize(.small) }
                    }
                    .buttonStyle(.borderless)
                    .disabled(loadingMore)
                }
            }
            .font(.caption)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal)
            .padding(.vertical, 6)
        }
    }
}

/// A Kubernetes list: the scope picker on top whatever the list's state, so a scope that
/// fails (no access to that namespace) or is missing can be changed (L6), the progress and
/// what an incomplete list means, then the rows (`content`) once loaded.
struct KubeListFrame<T: Codable & Sendable, Content: View>: View {
    let control: KubeScopeControl
    let list: PagedList<T>
    let query: String
    /// The namespaces of loaded rows, offered while the cluster's are unknown.
    let namespaces: ([T]) -> [String]
    @ViewBuilder let content: (PagedLoad<T>) -> Content

    @Environment(AppModel.self) private var model

    /// What decides the rows: the scope, and the cluster, API address and screenshot mode.
    private struct Trigger: Hashable {
        let scope: KubeScope
        let ready: Bool
        let context: String
        let server: String?
        let generation: Int
    }

    var body: some View {
        VStack(spacing: 0) {
            KubeScopeBar(control: control, loaded: loadedNamespaces)
                .background(.bar)
            Divider()
            if !control.ready {
                ContentUnavailableView {
                    Label("Namespace", systemImage: "square.dashed")
                } description: {
                    Text("Type the namespace to list above.")
                }
                .frame(maxHeight: .infinity)
            } else {
                LoadStateView(state: shownState, retry: { await list.refresh(model: model) }) { load in
                    VStack(spacing: 0) {
                        PagedProgressBar(progress: list.progress)
                        IncompleteNotice(load: load, searching: !query.isEmpty, loadingMore: list.loadingMore,
                                         onLoadMore: { list.loadMore(model: model) },
                                         onLoadAll: { list.loadMore(model: model, all: true) })
                        content(load)
                    }
                }
            }
        }
        .task(id: Trigger(scope: control.scope, ready: control.ready, context: model.activeContext,
                          server: model.client?.kubeServer, generation: model.dataGeneration)) {
            if control.ready { await list.show(control.scope, model: model) }
        }
    }

    private var loadedNamespaces: [String] {
        if case .loaded(let load, _, _) = list.state { return namespaces(load.items) }
        return []
    }

    /// The list's state, a namespace the credentials may not read told as such.
    private var shownState: LoadState<PagedLoad<T>> {
        guard case .failed(let message) = list.state, let namespace = control.scope.namespace, isKubeForbidden(message) else {
            return list.state
        }
        return .failed(String(localized: "No access to the namespace \(namespace): pick or type another one."))
    }
}

/// Asks for the next page of a list that stopped at its cap when its end shows: the last row
/// of the List. Not while searching: a match may then be on a page never loaded.
struct LoadMoreRow: View {
    let action: () -> Void

    var body: some View {
        HStack {
            Spacer()
            ProgressView()
            Spacer()
        }
        .listRowBackground(Color.clear)
        .onAppear(perform: action)
    }
}
