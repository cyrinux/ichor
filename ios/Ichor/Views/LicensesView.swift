import SwiftUI
import IchorCore

/// Third-party components and their licenses (IchorCore.openSourceLibraries); each license
/// opens its full text on spdx.org.
struct LicensesView: View {
    var body: some View {
        List(openSourceLibraries) { library in
            VStack(alignment: .leading, spacing: 4) {
                if let website = URL(string: library.website) {
                    Link(destination: website) { Text(verbatim: library.name).font(.headline) }
                        .buttonStyle(.borderless)
                } else {
                    Text(verbatim: library.name).font(.headline)
                }
                Text(verbatim: library.author).font(.caption).foregroundStyle(.secondary)
                HStack {
                    ForEach(library.licenses, id: \.self) { license in
                        if let url = URL(string: licenseURL(license)) {
                            Link(destination: url) { Text(verbatim: license).font(.caption.monospaced()) }
                                .buttonStyle(.bordered)
                        }
                    }
                }
            }
            .padding(.vertical, 2)
        }
        .themedBackground()
        .navigationTitle("Open-source licenses")
    }
}
