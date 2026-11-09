import SwiftUI
import IchorCore

extension Route {
    /// The Kubernetes screen's Pods tab on one pod, its logs open: where a pod named on any
    /// screen leads.
    static func pod(namespace: String, name: String) -> Route {
        .kubernetes(KubeFocus.pod(namespace: namespace, name: name))
    }
}

/// A row that opens a pod's logs and actions (the Kubernetes screen focused on it). Pushed as a
/// NavigationLink; from a sheet, give `open`, which closes the sheet before pushing the route
/// (a push from inside a sheet is dropped).
struct PodLink<Label: View>: View {
    let namespace: String
    let name: String
    let open: ((Route) -> Void)?
    let label: Label

    init(
        namespace: String, name: String, open: ((Route) -> Void)? = nil,
        @ViewBuilder label: () -> Label
    ) {
        self.namespace = namespace
        self.name = name
        self.open = open
        self.label = label()
    }

    init(pod: KubePod, open: ((Route) -> Void)? = nil, @ViewBuilder label: () -> Label) {
        self.init(namespace: pod.namespace, name: pod.name, open: open, label: label)
    }

    var body: some View {
        let route = Route.pod(namespace: namespace, name: name)
        if let open {
            Button {
                open(route)
            } label: {
                label
            }
            .tint(.primary)
            .accessibilityHint(Text("Open pod"))
        } else {
            NavigationLink(value: route) {
                label
            }
            .accessibilityHint(Text("Open pod"))
        }
    }
}

/// A sheet over `selected` whose pod links close it first: the pod is kept in `pendingPod`, then
/// the sheet's dismissal moves it to `openPod`, which `podDestination` pushes. A push from inside
/// a sheet is dropped.
struct PodSheet<Item: Identifiable, Sheet: View>: ViewModifier {
    @Binding var selected: Item?
    @Binding var pendingPod: KubeFocus?
    @Binding var openPod: KubeFocus?
    /// The sheet for an item, given the closure its pod links call.
    let sheet: (Item, @escaping (Route) -> Void) -> Sheet

    func body(content: Content) -> some View {
        content
            .sheet(item: $selected, onDismiss: dismissed) { item in
                sheet(item, open)
            }
            .podDestination($openPod)
    }

    private func open(_ route: Route) {
        guard case .kubernetes(let focus) = route else { return }
        pendingPod = focus
        selected = nil
    }

    private func dismissed() {
        openPod = pendingPod
        pendingPod = nil
    }
}

extension View {
    /// Pushes the Kubernetes screen focused on the pod a sheet asked for, set once the sheet is
    /// closed (in its `onDismiss`): a push from inside a sheet is dropped.
    func podDestination(_ focus: Binding<KubeFocus?>) -> some View {
        navigationDestination(item: focus) {
            KubernetesView(focus: $0).recordsOpened($0.shareTarget, label: $0.name, detail: $0.namespace)
        }
    }
}
