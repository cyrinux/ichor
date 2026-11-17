import SwiftUI
import IchorCore

/// The Kubernetes clusters of the Teleport proxy `target` signs in at (its stored contexts): a
/// tap opens one's sign-in in the same sheet (`onOpen`), one can be added by name (`onAdded`
/// gets its new context), and the proxy can be signed out of entirely (`onSignedOut`).
/// Nothing for a cluster that lists none (the demo). Same section on Android.
struct KubeTeleportClustersSection: View {
    let target: KubeSignInTarget
    var onOpen: (String) -> Void
    var onAdded: (String) -> Void
    var onSignedOut: () -> Void

    @Environment(AppModel.self) private var model
    @State private var list: KubeTeleportClusters?
    @State private var name = ""
    @State private var addError: String?
    @State private var busy = false
    @State private var confirmSignOut = false

    var body: some View {
        Group {
            if let list, !list.clusters.isEmpty {
                clustersSection(list)
                addSection
            }
        }
        .task(id: target.id) { await load() }
        .confirmationDialog(
            "Sign out of this Teleport proxy", isPresented: $confirmSignOut, titleVisibility: .visible
        ) {
            Button("Sign out", role: .destructive) { Task { await signOutAll() } }
        } message: {
            Text(
                "This removes every certificate of this proxy from the phone. Each of its clusters asks you to sign in again."
            )
        }
    }

    private func clustersSection(_ list: KubeTeleportClusters) -> some View {
        Section {
            ForEach(list.clusters, id: \.self) { cluster in
                Button {
                    onOpen(cluster.context)
                } label: {
                    LabeledContent {
                        Text(status(cluster))
                    } label: {
                        Text(verbatim: cluster.name)
                    }
                }
                .disabled(cluster.context == target.context)
                .accessibilityAddTraits(cluster.context == target.context ? .isSelected : [])
            }
            if list.signedInAnywhere {
                Button("Sign out of this Teleport proxy", role: .destructive) { confirmSignOut = true }
            }
        } header: {
            Text("Kubernetes clusters at this proxy")
        }
    }

    private var addSection: some View {
        Section {
            TextField("Kubernetes cluster name", text: $name)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .disabled(busy)
            Button("Add a cluster by name") { Task { await add() } }
                .disabled(busy || name.trimmingCharacters(in: .whitespaces).isEmpty)
            if let addError {
                Text(addError).font(.footnote).foregroundStyle(.statusBad)
            }
        } footer: {
            Text(
                "Only the clusters added here are listed: add another by its name in Teleport. Its sign-in opens the browser again; your identity provider usually skips the password."
            )
        }
    }

    private func status(_ cluster: KubeTeleportCluster) -> String {
        guard cluster.signedIn else { return String(localized: "No certificate") }
        guard cluster.expires > 0 else { return String(localized: "Signed in") }
        let until = Date(timeIntervalSince1970: TimeInterval(cluster.expires)).formatted(
            date: .abbreviated, time: .shortened)
        return String(localized: "Certificate until \(until)")
    }

    private func load() async {
        list = try? await TalosClient.teleportClusters(kube: target.kube, context: target.context)
    }

    private func add() async {
        busy = true
        defer { busy = false }
        do {
            let context = try await model.addTeleportCluster(
                from: target.context, kubeCluster: name.trimmingCharacters(in: .whitespaces))
            name = ""
            addError = nil
            onAdded(context)
        } catch {
            addError = error.localizedDescription
        }
    }

    private func signOutAll() async {
        try? await TalosClient.teleportSignOutAll(kube: target.kube, context: target.context)
        model.reloadKubernetes()
        await load()
        onSignedOut()
    }
}
