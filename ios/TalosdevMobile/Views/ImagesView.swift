import SwiftUI
import TalosdevMobileCore

/// Container images of a node's Kubernetes (CRI) namespace, like `talosctl image list`
/// (os:reader): searchable, sortable by name, size or age.
struct ImagesView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[ContainerImage]> = .loading
    @State private var sort = ImageSort.name
    @State private var query = ""

    var body: some View {
        LoadStateView(state: state, retry: load) { images in
            let shown = sortImages(filterImages(images, query: query), by: sort)
            List {
                Section {
                    LabeledContent("Images", value: query.isEmpty ? "\(images.count)" : "\(shown.count) / \(images.count)")
                    LabeledContent("Total size", value: ByteCountFormatter.string(fromByteCount: totalImageSize(shown), countStyle: .file))
                    Picker("Sort by", selection: $sort) {
                        ForEach(ImageSort.allCases, id: \.self) { Text($0.localizedLabel).tag($0) }
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    Text("Images share layers, so the total can exceed the disk space they use.")
                }
                Section {
                    ForEach(shown) { ImageRow(image: $0) }
                }
            }
            .overlay {
                if shown.isEmpty {
                    if query.isEmpty {
                        ContentUnavailableView("No images", systemImage: "shippingbox")
                    } else {
                        ContentUnavailableView.search(text: query)
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .searchable(text: $query, prompt: Text("Image name or digest"))
        .navigationTitle(String(localized: "Images · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.images(node: node) }
    }
}

private struct ImageRow: View {
    let image: ContainerImage

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: image.name)
                .font(.callout.monospaced())
                .lineLimit(2)
                .truncationMode(.middle)
                .textSelection(.enabled)
            HStack(spacing: 12) {
                Text(verbatim: ByteCountFormatter.string(fromByteCount: image.size, countStyle: .file))
                if image.created > 0 {
                    Text(Date(timeIntervalSince1970: TimeInterval(image.created) / 1000), format: .dateTime.year().month().day())
                }
                Text(verbatim: shortDigest).font(.caption.monospaced())
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .monospacedDigit()
        }
    }

    /// "sha256:0123456789ab" (the first 12 hex digits, like docker).
    private var shortDigest: String {
        guard let colon = image.digest.firstIndex(of: ":") else { return String(image.digest.prefix(12)) }
        let hex = image.digest[image.digest.index(after: colon)...]
        return "\(image.digest[..<colon]):\(hex.prefix(12))"
    }
}
