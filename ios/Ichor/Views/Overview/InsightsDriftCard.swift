import SwiftUI
import UIKit
import IchorCore

/// One configuration difference as a Section: the node (hostname first, IP muted), the setting as
/// a Tag and, for a list-valued setting, one row per added, removed or changed item. A setting
/// with more than three items starts collapsed. Scalar settings, and every change from a core
/// older than the itemised diff, keep the single before → after row.
struct DriftChangeSection: View {
    let change: DriftChange
    /// The draft patch editor, when the difference can be fixed with one.
    let fixDestination: AnyView?
    @State private var expanded = false

    var body: some View {
        Section {
            if change.itemCount > 3 {
                DisclosureGroup(isExpanded: $expanded) {
                    items
                } label: {
                    header
                }
            } else {
                header
                if change.isItemised { items } else { scalarRow }
            }
            Text("Reference: \(referenceLabel)").font(.caption).foregroundStyle(.secondary)
            if let fixDestination {
                NavigationLink {
                    fixDestination
                } label: {
                    Text("Fix with a patch…").font(.caption)
                }
            }
        }
    }

    private var referenceLabel: String {
        change.referenceName.isEmpty ? change.reference : "\(change.referenceName) (\(change.reference))"
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(verbatim: change.nodeName.isEmpty ? change.node : change.nodeName).font(.headline)
                    if !change.nodeName.isEmpty {
                        Text(verbatim: change.node).font(.caption).foregroundStyle(.secondary)
                    }
                }
                if change.isItemised {
                    Text("\(change.itemCount) differences").font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            Tag(change.key, mono: true)
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var items: some View {
        ForEach(Array(change.added.enumerated()), id: \.offset) { _, value in
            DriftDiffRow(
                sign: "+", text: shortDriftValue(value), color: .statusOK,
                label: Text("Added"), copy: value)
        }
        ForEach(Array(change.removed.enumerated()), id: \.offset) { _, value in
            DriftDiffRow(
                sign: "−", text: shortDriftValue(value), color: .statusBad,
                label: Text("Removed"), copy: value)
        }
        ForEach(Array(change.changed.enumerated()), id: \.offset) { _, pair in
            DriftDiffRow(
                sign: "~",
                text: "\(pair.name) \(shortDriftValue(pair.before)) → \(shortDriftValue(pair.after))",
                color: .statusWarn, label: Text("Changed"),
                copy: "\(pair.name) \(pair.before) → \(pair.after)")
        }
    }

    private var scalarRow: some View {
        let line = "\(change.before.isEmpty ? "∅" : change.before) → \(change.after.isEmpty ? "∅" : change.after)"
        return Text(verbatim: line)
            .font(.caption.monospaced())
            .contextMenu {
                Button {
                    UIPasteboard.general.string = line
                } label: {
                    Label("Copy", systemImage: "doc.on.doc")
                }
            }
    }
}

/// A `+`, `−` or `~` row in its status colour. VoiceOver reads what the sign means; the full
/// value is one long-press away.
private struct DriftDiffRow: View {
    let sign: String
    let text: String
    let color: Color
    let label: Text
    let copy: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(verbatim: sign).font(.caption.monospaced().bold()).foregroundStyle(color)
            Text(verbatim: text).font(.caption.monospaced()).foregroundStyle(color)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label + Text(verbatim: ": \(text)"))
        .contextMenu {
            Button {
                UIPasteboard.general.string = copy
            } label: {
                Label("Copy", systemImage: "doc.on.doc")
            }
        }
    }
}
