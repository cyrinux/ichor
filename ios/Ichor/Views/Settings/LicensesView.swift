import SwiftUI
import IchorCore

/// About › Open-source licenses: the project's NOTICE and LICENSE, then every bundled library
/// (Go modules, the Go standard library, Swift packages) with its license text.
struct LicensesView: View {
    @State private var licenses = OpenSourceLicenses()

    var body: some View {
        List {
            if !licenses.notice.isEmpty {
                Section {
                    NavigationLink {
                        LicenseText(title: "Ichor", website: ProjectLinks.repo.absoluteString, text: licenses.notice)
                    } label: {
                        LibraryRow(name: "Ichor", summary: "Apache-2.0")
                    }
                }
            }
            Section {
                ForEach(licenses.libraries) { library in
                    NavigationLink {
                        LicenseText(title: library.name, website: library.website, text: library.text)
                    } label: {
                        LibraryRow(name: library.name, summary: library.summary)
                    }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Open-source licenses")
        .task {
            licenses = decodeOpenSourceLicenses(
                Bundle.main.url(forResource: "licenses", withExtension: "json").flatMap { try? Data(contentsOf: $0) }
            )
        }
    }
}

private struct LibraryRow: View {
    let name: String
    let summary: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: name).lineLimit(2)
            Text(verbatim: summary).font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// The license files verbatim; the toolbar links to the project's website.
private struct LicenseText: View {
    let title: String
    let website: String
    let text: String

    var body: some View {
        ScrollView {
            Text(verbatim: text)
                .font(.caption.monospaced())
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
        }
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let url = URL(string: website), !website.isEmpty {
                Link(destination: url) { Image(systemName: "safari") }
            }
        }
    }
}
