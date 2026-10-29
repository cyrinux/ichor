import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// A boot medium as a list section: `title`, an optional `hint`, its `values` (segmented when
/// more than one) in monospace, and Copy, Share and QR code for the shown one. `sensitive`: the
/// value carries a secret (Omni's join token), copied to this device only and for two minutes.
struct BootMediaCard: View {
    let title: String
    let values: [BootValue]
    var hint: String?
    var sensitive = false
    @State private var selected = 0
    @State private var showingQR = false

    private var shown: BootValue { values[min(selected, values.count - 1)] }
    /// The card and the tab, for the buttons' labels and the share and QR titles.
    private var name: String { [title, shown.label].filter { !$0.isEmpty }.joined(separator: " · ") }

    var body: some View {
        Section {
            if let hint { Text(verbatim: hint).font(.footnote).foregroundStyle(.secondary) }
            if sensitive {
                Text("Contains Omni's join token: share it only with people who may add machines.")
                    .font(.footnote).foregroundStyle(.orange)
            }
            if values.count > 1 {
                Picker(selection: $selected) {
                    ForEach(values.indices, id: \.self) { Text(verbatim: values[$0].label).tag($0) }
                } label: {
                    Text(verbatim: title)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
            }
            Text(verbatim: shown.text)
                .font(.caption.monospaced())
                .textSelection(.enabled)
            HStack(spacing: 20) {
                Spacer()
                Button(action: copy) { Image(systemName: "doc.on.doc") }
                    .accessibilityLabel(Text("Copy \(name)"))
                ShareLink(item: shown.text) { Image(systemName: "square.and.arrow.up") }
                    .accessibilityLabel(Text("Share \(name)"))
                Button {
                    showingQR = true
                } label: {
                    Image(systemName: "qrcode")
                }
                .accessibilityLabel(Text("QR code of \(name)"))
            }
            .buttonStyle(.borderless)
        } header: {
            Text(verbatim: title)
        }
        .sheet(isPresented: $showingQR) { BootMediaQRSheet(title: name, text: shown.text) }
    }

    /// Device-only pasteboard; the join token expires from it after two minutes.
    private func copy() {
        let options: [UIPasteboard.OptionsKey: Any] =
            sensitive
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: shown.text]], options: options)
    }
}

/// `text` as a QR code for the laptop next to the machine, or why it does not fit one.
struct BootMediaQRSheet: View {
    let title: String
    let text: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                if let image = makeQRCode(text) {
                    Image(uiImage: image)
                        .interpolation(.none)
                        .resizable()
                        .scaledToFit()
                        .padding(8)
                        .background(Color.white, in: RoundedRectangle(cornerRadius: 8))
                        .accessibilityLabel(Text("QR code of \(title)"))
                } else {
                    Text("Too long for a QR code: use Copy or Share.").note()
                }
                Spacer()
            }
            .padding()
            .navigationTitle(Text(verbatim: title))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
    }
}
