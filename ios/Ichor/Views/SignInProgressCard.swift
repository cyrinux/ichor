import IchorCore
import SwiftUI

/// The clusters found in a cloud account being signed in after the import, one after the
/// other: how many, a bar and the one being signed in.
struct SignInProgressCard: View {
    let progress: SignInProgress

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Adding \(String(progress.total)) clusters").font(.headline)
            ProgressView(value: progress.fraction)
            if !progress.current.isEmpty {
                Text("Signing in to \(progress.current)…")
                    .font(.footnote).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            }
            if !progress.failed.isEmpty {
                Text(verbatim: progress.failed.joined(separator: ", ")).font(.footnote).foregroundStyle(.statusBad)
            }
        }
        .padding()
        .background(
            Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12, style: .continuous)
        )
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .animation(.default, value: progress)
        .accessibilityElement(children: .combine)
    }
}
