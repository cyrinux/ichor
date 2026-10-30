import SafariServices
import SwiftUI

/// The in-app browser. It loads the page itself, where openURL or a Link hands a universal link
/// to the app claiming it: the GitHub app takes github.com/…/issues/new, drops the issue form
/// (fields and labels) and the CRD bot never sees the request. In PortForwardView the app stays
/// in the foreground, so the forward keeps running.
struct SafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        SFSafariViewController(url: url)
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}

extension View {
    /// Presents url, while it is set, in the in-app browser.
    func safariSheet(_ url: Binding<URL?>) -> some View {
        sheet(isPresented: url.isPresent()) {
            if let page = url.wrappedValue { SafariView(url: page).ignoresSafeArea() }
        }
    }
}
