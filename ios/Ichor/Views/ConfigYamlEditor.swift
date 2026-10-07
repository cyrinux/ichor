import SwiftUI
import IchorCore

/// The machine config as numbered YAML lines, filtered by the search. Read-only.
struct ConfigYamlLines: View {
    let yaml: String
    let query: String
    let refresh: () async -> Void

    var body: some View {
        let lines = filterLines(yaml, query: query)
        List(lines) { line in
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(verbatim: "\(line.number)")
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(.tertiary)
                    .frame(minWidth: 28, alignment: .trailing)
                Text(verbatim: line.text)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .listRowInsets(EdgeInsets(top: 1, leading: 8, bottom: 1, trailing: 8))
            .listRowSeparator(.hidden)
        }
        .listStyle(.plain)
        .environment(\.defaultMinListRowHeight, 0)
        .overlay {
            if lines.isEmpty && !query.isEmpty { ContentUnavailableView.search(text: query) }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

/// The draft as editable YAML, with the syntax error of what is typed above it.
struct ConfigYamlEditor: View {
    @Binding var text: String
    let error: ConfigSyntaxError?

    var body: some View {
        VStack(spacing: 0) {
            if let error {
                ConfigSyntaxErrorRow(error: error)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal)
                    .padding(.vertical, 6)
            }
            PlainTextEditor(text: $text)
        }
        .themedBackground()
    }
}

/// "Line 12: mapping values are not allowed here", in the warning tint.
struct ConfigSyntaxErrorRow: View {
    let error: ConfigSyntaxError

    var body: some View {
        Label {
            Text(verbatim: configSyntaxText(error))
        } icon: {
            Image(systemName: "exclamationmark.triangle.fill")
        }
        .font(.footnote)
        .foregroundStyle(.statusWarn)
    }
}

func configSyntaxText(_ error: ConfigSyntaxError) -> String {
    error.line > 0 ? String(localized: "Line \(error.line): \(error.message)") : error.message
}

/// A monospaced text view that leaves what is typed alone: SwiftUI's TextEditor cannot turn
/// off smart quotes and dashes, which break YAML.
private struct PlainTextEditor: UIViewRepresentable {
    @Binding var text: String

    func makeCoordinator() -> Coordinator { Coordinator(text: $text) }

    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.delegate = context.coordinator
        view.font = UIFontMetrics(forTextStyle: .caption1)
            .scaledFont(for: .monospacedSystemFont(ofSize: 12, weight: .regular))
        view.adjustsFontForContentSizeCategory = true
        view.autocorrectionType = .no
        view.autocapitalizationType = .none
        view.spellCheckingType = .no
        view.smartQuotesType = .no
        view.smartDashesType = .no
        view.smartInsertDeleteType = .no
        view.keyboardDismissMode = .interactive
        view.alwaysBounceVertical = true
        view.backgroundColor = .clear
        view.textContainerInset = UIEdgeInsets(top: 8, left: 8, bottom: 8, right: 8)
        view.text = text
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        context.coordinator.text = $text
        // Only a change made elsewhere (a field edit) replaces the text: the caret stays put while typing.
        if view.text != text { view.text = text }
    }

    final class Coordinator: NSObject, UITextViewDelegate {
        var text: Binding<String>

        init(text: Binding<String>) { self.text = text }

        func textViewDidChange(_ textView: UITextView) {
            text.wrappedValue = textView.text
        }
    }
}
