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
