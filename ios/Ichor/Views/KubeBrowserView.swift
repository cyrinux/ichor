import SwiftUI
import IchorCore

/// Every kind the cluster serves, CRDs included, from the API server's discovery: searchable
/// by kind, plural, short name or group, grouped by API group (core and apps first). A kind
/// opens its list with the server's own columns, as `kubectl get` shows them. Works the same
/// for a Talos cluster and a kubeconfig one: only the Kubernetes API is asked.
struct KubeBrowserView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeAPIResourceList> = .loading
    @State private var query = ""

    var body: some View {
        LoadStateView(state: state, retry: load) { list in
            let sections = groupAPIResources(list.resources, query: query)
            List {
                if query.isEmpty {
                    Section {
                        NavigationLink { HelmReleasesView() } label: {
                            Label("Helm releases", systemImage: "shippingbox")
                        }
                    }
                }
                if !list.failed.isEmpty && query.isEmpty {
                    Section {
                        Label {
                            Text("Some API groups could not be read: \(list.failed.joined(separator: ", "))")
                        } icon: {
                            Image(systemName: "exclamationmark.triangle")
                        }
                        .font(.footnote)
                        .foregroundStyle(.statusWarn)
                    }
                }
                ForEach(sections) { section in
                    Section {
                        ForEach(section.resources) { resource in
                            NavigationLink { KubeResourceListView(resource: resource) } label: {
                                KubeKindRow(resource: resource)
                            }
                        }
                    } header: {
                        if section.group.isEmpty {
                            Text("Core")
                        } else {
                            Text(verbatim: section.group)
                        }
                    }
                }
            }
            .emptyOverlay(sections.isEmpty, query: query) {
                ContentUnavailableView("No resources", systemImage: "square.grid.3x3")
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .searchable(text: $query, prompt: Text("Kind, short name or group"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text("Resources"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: kubeNamespacesKey(model)) { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<KubeAPIResourceList> = await .from { try await client.apiResources() }
        guard key == kubeNamespacesKey(model) else { return }
        state = state.refreshed(with: fetched)
    }
}

/// A kind: its name, plural and group version, short names, and whether it lives in a namespace.
private struct KubeKindRow: View {
    let resource: KubeAPIResource

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: resource.kind).font(.body.weight(.medium))
                Spacer(minLength: 4)
                if !resource.namespaced {
                    Text("Cluster")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.accentColor.opacity(0.14), in: Capsule())
                        .foregroundStyle(.tint)
                }
            }
            HStack(spacing: 6) {
                Text(verbatim: "\(resource.resource) · \(resource.groupVersion)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                ForEach(resource.shortNames.prefix(3), id: \.self) { short in
                    Text(verbatim: short)
                        .font(.caption2.monospaced())
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}
