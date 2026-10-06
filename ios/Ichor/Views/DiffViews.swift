import SwiftUI
import IchorCore

// What a GitOps tool would change, object by object: shared by the Flux diff and, later,
// Argo CD's. Each object is a row with its change badge; its unified diff unfolds below in red
// and green lines, scrolling sideways rather than wrapping (YAML indentation matters).

extension DiffChange {
    var label: String {
        switch self {
        case .created: String(localized: "Created")
        case .changed: String(localized: "Changed")
        case .deleted: String(localized: "Deleted")
        case .encrypted: String(localized: "Encrypted")
        case .ignored: String(localized: "Ignored")
        case .error: String(localized: "Error")
        case .unchanged: String(localized: "Unchanged")
        }
    }

    /// Green created, amber changed, red deleted and errors, grey otherwise.
    var color: Color {
        switch self {
        case .created: .statusOK
        case .changed: .statusWarn
        case .deleted, .error: .statusBad
        case .encrypted, .ignored, .unchanged: .secondary
        }
    }

    var symbol: String {
        switch self {
        case .created: "plus.circle"
        case .changed: "pencil.circle"
        case .deleted: "minus.circle"
        case .encrypted: "lock"
        case .ignored: "eye.slash"
        case .error: "exclamationmark.triangle"
        case .unchanged: "checkmark.circle"
        }
    }

    /// Why an object is not compared, for the changes that say nothing by themselves.
    var hint: String? {
        switch self {
        case .encrypted: String(localized: "Encrypted with SOPS: not compared, the key stays in the cluster.")
        case .ignored: String(localized: "Flux leaves it alone (reconcile disabled, ignored, or created only once).")
        default: nil
        }
    }
}

/// The change as a small tinted label, with count when given ("Changed 3").
struct DiffChangeBadge: View {
    let change: DiffChange
    var count: Int?

    var body: some View {
        Label {
            Text(verbatim: count.map { "\(change.label) \($0)" } ?? change.label)
        } icon: {
            Image(systemName: change.symbol)
        }
        .font(.caption.weight(.semibold))
        .labelStyle(.titleAndIcon)
        .padding(.horizontal, 7)
        .padding(.vertical, 3)
        .foregroundStyle(change.color)
        .background(change.color.opacity(0.14), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}

/// One object: its badge, kind, name and namespace; it unfolds its diff (or why it was refused,
/// or why it is not compared) when it has one.
struct DiffResourceRow: View {
    let resource: KubeDiffResource
    @Binding var expanded: Bool

    private var hasDetail: Bool { !resource.diff.isEmpty || !resource.error.isEmpty || resource.change.hint != nil }

    var body: some View {
        if hasDetail {
            DisclosureGroup(isExpanded: $expanded) { detail } label: { header }
        } else {
            header
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                DiffChangeBadge(change: resource.change)
                Text(verbatim: resource.kind).font(.subheadline.weight(.semibold)).lineLimit(1)
            }
            Text(verbatim: resource.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
            if !resource.namespace.isEmpty {
                Text(verbatim: resource.namespace).font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var detail: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let hint = resource.change.hint {
                Text(hint).font(.caption).foregroundStyle(.secondary)
            }
            if !resource.error.isEmpty {
                Text("The API server refused it: \(resource.error)").font(.caption).foregroundStyle(.statusBad).textSelection(.enabled)
            }
            if !resource.diff.isEmpty { DiffLinesView(lines: resource.lines) }
            if resource.truncated {
                Text("Diff cut at 64 KB.").font(.caption2).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}

/// The lines of a unified diff, monospace, scrolling sideways together.
struct DiffLinesView: View {
    let lines: [DiffLine]

    var body: some View {
        ScrollView(.horizontal) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                    row(line)
                }
            }
            .padding(.vertical, 6)
        }
        .background(Color(.tertiarySystemFill), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .textSelection(.enabled)
    }

    private func row(_ line: DiffLine) -> some View {
        let style: (sign: String, tint: Color) = switch line.kind {
        case .added: ("+", Color.statusOK)
        case .removed: ("-", Color.statusBad)
        case .hunk: ("", Color.secondary)
        case .context: (" ", Color.primary)
        }
        let tinted = line.kind == .added || line.kind == .removed
        return HStack(spacing: 0) {
            Text(verbatim: style.sign).frame(width: 12, alignment: .leading)
            Text(verbatim: line.text).fixedSize()
        }
        .font(.caption.monospaced())
        .foregroundStyle(style.tint)
        .padding(.horizontal, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(tinted ? style.tint.opacity(0.12) : Color.clear)
    }
}
