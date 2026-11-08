import SwiftUI
import IchorCore

/// "Forget node" for a node that does not answer and that the cluster no longer lists; nothing
/// for any other node. Checked again when reachability changes: a read that fails offers nothing.
struct NodeForgetSection: View {
    let ref: NodeRef
    let onForgotten: () -> Void

    @Environment(AppModel.self) private var model
    @Environment(NodesStore.self) private var nodes
    @State private var removal: ContextNodeRemoval?
    @State private var confirming = false
    @State private var failure: String?

    var body: some View {
        Group {
            if removal != nil {
                Section {
                    Button("Forget node", role: .destructive) { confirming = true }
                } footer: {
                    Text("This node does not answer and the cluster no longer lists it.")
                }
            }
        }
        .task(id: nodes.reachability(of: ref.address).isDown) { await check() }
        .confirmationDialog(
            String(localized: "Forget \(ref.hostname)?"), isPresented: $confirming, titleVisibility: .visible
        ) {
            Button("Forget node", role: .destructive) { Task { await forget() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(verbatim: confirmationMessage)
        }
        .alert(failure ?? "", isPresented: $failure.isPresent()) {
            Button("OK") {}
        }
    }

    private var confirmationMessage: String {
        let body = String(localized: "The app stops targeting this node. The machine itself is not changed.")
        guard removal?.endpoint == true else { return body }
        let endpoint = String(
            localized: "The app also connects through this address: change the endpoints if it no longer answers.")
        return body + " " + endpoint
    }

    private func check() async {
        guard nodes.reachability(of: ref.address).isDown, let client = model.client,
            (try? await client.forgettable(node: ref.address)) == true
        else {
            removal = nil
            return
        }
        removal = (try? await client.removalPlan(node: ref.address)).flatMap { $0.allowed ? $0 : nil }
    }

    private func forget() async {
        do {
            let context = model.activeContext
            try await model.removeNodes([ref.address])
            nodes.noteForgotten([ref.address, ref.hostname], of: context)
            onForgotten()
        } catch {
            failure = String(localized: "\(ref.hostname): not removed: \(error.localizedDescription)")
        }
    }
}
