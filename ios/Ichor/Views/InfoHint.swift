import SwiftUI

/// An (i) next to a label: tapping it explains the feature or how to read a value. With a
/// link, the explanation also offers that page of the documentation.
struct InfoHint: View {
    let title: Text
    let text: Text
    var link: URL?
    @State private var open = false
    @Environment(\.openURL) private var openURL

    init(_ title: LocalizedStringKey, _ text: LocalizedStringKey, link: URL? = nil) {
        self.init(title: Text(title), text: Text(text), link: link)
    }

    init(title: Text, text: Text, link: URL? = nil) {
        self.title = title
        self.text = text
        self.link = link
    }

    var body: some View {
        Button {
            open = true
        } label: {
            Image(systemName: "info.circle")
                .imageScale(.medium)
                .foregroundStyle(.secondary)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
        }
        // Borderless, so a tap inside a list row or a section header only opens the hint.
        .buttonStyle(.borderless)
        .accessibilityLabel(Text("More information"))
        .alert(title, isPresented: $open) {
            if let link {
                Button("Documentation") { openURL(link) }
            }
            Button("OK") {}
        } message: {
            text
        }
    }
}

/// A muted line closing a list, whose (i) tells how a resource names its own icon.
struct CustomIconHint: View {
    let text: LocalizedStringKey
    let link: URL

    /// The website's section on CronJobs: their icon, name and description labels.
    static let cronJobsDocs = URL(string: "https://cyrinux.github.io/ichor/#cronjobs")!
    /// The website's section on Argo CD: an app's own icon.
    static let argoDocs = URL(string: "https://cyrinux.github.io/ichor/#argocd")!

    static let cronJobs: LocalizedStringKey =
        "Give a CronJob its own icon with the ichor.levis.name/icon label or annotation, set to a Dashboard Icons name such as postgresql. The ichor.levis.name/title and ichor.levis.name/description annotations set its name and a line of description.\n\nkubectl label cronjob db-backup ichor.levis.name/icon=postgresql\n\nWithout one, Ichor picks the icon of the app its image belongs to, else a clock."
    static let argo: LocalizedStringKey =
        "An app Ichor does not recognise can bring its own logo with the ichor.levis.name/icon annotation: a Dashboard Icons name, an https link to a PNG or WebP (downloaded only when icon downloads are on) or the image itself, inline in base64. A label holds a Dashboard Icons name only.\n\nkubectl -n argocd annotate application my-site ichor.levis.name/icon=grafana"

    var body: some View {
        HStack(spacing: 0) {
            Text("Custom icon")
                .font(.caption)
                .foregroundStyle(.secondary)
            InfoHint(title: Text("Custom icon"), text: Text(text), link: link)
        }
        .frame(maxWidth: .infinity)
    }
}
