import SwiftUI

/// "Find spare parts" for one part of a node: opens the Ichor site's parts page in the browser,
/// where the vendor's page and a web search are one tap away. Nothing without a link (a virtual
/// part, or one with neither a model nor a serial).
struct FindPartsButton: View {
    let url: URL?

    @Environment(\.openURL) private var openURL

    init(kind: String, manufacturer: String, model: String, serial: String) {
        url = TalosClient.partsLookupURL(kind: kind, manufacturer: manufacturer, model: model, serial: serial)
    }

    var body: some View {
        if let url {
            Button { openURL(url) } label: {
                Label("Find spare parts", systemImage: "wrench.and.screwdriver")
            }
            .accessibilityHint(Text("Opens the Ichor site with this part's model and serial number, then the vendor's page or a web search"))
        }
    }
}
