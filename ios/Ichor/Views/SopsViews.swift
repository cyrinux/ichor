import SwiftUI
import IchorCore

/// A SOPS-encrypted file in the viewer: who it is encrypted for (each key by its ends), when it
/// was last encrypted, how many values are, and the ones meant to be still in clear. The phone
/// never decrypts it: it is edited on a computer with sops.
struct SopsBar: View {
    let info: SopsInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label("Encrypted with SOPS", systemImage: "lock.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.primary)
            if !info.recipients.isEmpty {
                Text(verbatim: info.recipients.map { "\($0.type) \($0.short)" }.joined(separator: " · "))
                    .font(.caption.monospaced())
            }
            Text("Encrypted values: \(info.encryptedKeys)")
            if !info.lastModified.isEmpty {
                Text("Last encrypted \(info.lastModified)")
            }
            if !info.clearKeys.isEmpty {
                Text("In clear although the file's rules encrypt them: \(info.clearKeys.joined(separator: ", "))")
                    .foregroundStyle(.statusBad)
            }
            Text("The phone never decrypts it: edit it on a computer with sops.")
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }
}

/// The edit commits a clear-text secret where the repository's .sops.yaml encrypts.
struct SopsWarningText: View {
    let warning: SopsWarning

    var body: some View {
        let recipients = warning.rule?.recipients.map { "\($0.type) \($0.short)" }.joined(separator: ", ") ?? ""
        Label {
            Text("This path is encrypted by SOPS in this repository (rule \(warning.rule?.pathRegex ?? ""), recipients \(recipients.isEmpty ? "-" : recipients)): \(sopsWhat(warning)) would be committed in clear text.")
        } icon: {
            Image(systemName: "lock.open.trianglebadge.exclamationmark")
        }
        .foregroundStyle(.statusBad)
    }
}

/// What a warning names, worded: a Kubernetes Secret, or the Talos key as is.
func sopsWhat(_ warning: SopsWarning) -> String {
    if warning.isSecret { return String(localized: "a Kubernetes Secret") }
    if warning.isTalosBundle { return String(localized: "Talos secrets") }
    return warning.what
}
