import SwiftUI
import IchorCore

/// What a Teleport sign-in goes to (the proxy, the Teleport and Kubernetes clusters, the SSO
/// connector last used), read-only, and how the sign-in works. Same rows on Android.
struct KubeTeleportDetails: View {
    let info: KubeSignInInfo

    var body: some View {
        Section {
            ForEach(info.teleportValues, id: \.self) { item in
                LabeledContent {
                    Text(verbatim: item.value).font(.callout.monospaced())
                } label: {
                    Text(Self.label(item.key))
                }
            }
        } footer: {
            Text(
                "Signs in at your Teleport proxy in the browser and keeps a short-lived certificate; when it expires you sign in again."
            )
        }
    }

    private static func label(_ key: String) -> LocalizedStringKey {
        switch key {
        case "teleportProxy": "Proxy"
        case "teleportCluster": "Teleport cluster"
        case "kubeCluster": "Kubernetes cluster"
        default: "Sign-in connector"
        }
    }
}
