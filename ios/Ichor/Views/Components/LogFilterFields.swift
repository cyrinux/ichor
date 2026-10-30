import SwiftUI
import IchorCore

/// A text or regular expression filter and the time range, for a log sheet. The field keeps
/// its own text so typing stays smooth: `filter` follows it half a second after the last key,
/// since each change reads the log again.
struct LogFilterFields: View {
    @Binding var filter: LogFilter
    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                TextField("Filter lines", text: $text)
                    .textFieldStyle(.roundedBorder)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                Toggle(isOn: $filter.regex) {
                    Text(verbatim: ".*").monospaced()
                }
                .toggleStyle(.button)
                .accessibilityLabel(Text("Regular expression"))
            }
            Picker("Time range", selection: $filter.sinceMinutes) {
                ForEach(logSinceChoices, id: \.self) { minutes in
                    sinceLabel(minutes).tag(minutes)
                }
            }
        }
        .onAppear { text = filter.text }
        .task(id: text) {
            guard text != filter.text else { return }
            try? await Task.sleep(for: .milliseconds(500))
            if !Task.isCancelled { filter.text = text }
        }
    }

    private func sinceLabel(_ minutes: Int) -> Text {
        if minutes == 0 { return Text("All") }
        return minutes < 60 ? Text("\(minutes) min") : Text("\(minutes / 60) h")
    }
}
