import SwiftUI
import IchorCore

/// In place of the overview while the cluster lists no endpoint (a talosconfig generated before
/// it had addresses): find its nodes on the networks around the phone, or type a node's address.
struct NoEndpointsView: View {
    let context: ContextSummary

    @Environment(AppModel.self) private var model
    @State private var editing: ContextSummary?
    @State private var scanning = false

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                Image(systemName: "network")
                    .font(.system(size: 44))
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                Text("No endpoint yet").font(.title2.bold()).multilineTextAlignment(.center).accessibilityAddTraits(.isHeader)
                Text("This talosconfig lists no address to reach the cluster. Search the networks around the phone for its nodes, or add a node's address.")
                    .font(.callout)
                    .multilineTextAlignment(.center)
                Button("Search the local network") { scanning = true }
                    .buttonStyle(.borderedProminent)
                    .padding(.top, 8)
                // In screenshot mode the editor would show, and save, fake endpoints.
                if !model.labels.masked {
                    Button("Add an endpoint") { editing = context }.buttonStyle(.bordered)
                }
            }
            .padding(32)
            .frame(maxWidth: .infinity)
        }
        .endpointTools(editing: $editing, scanning: $scanning)
    }
}
