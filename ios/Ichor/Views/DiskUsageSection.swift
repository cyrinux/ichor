import SwiftUI
import IchorCore

/// Disk usage explorer, like `talosctl usage`: the sizes under a path, largest first; a
/// directory row drills into it, the path bar and the shortcuts jump elsewhere. Nothing is
/// measured until a folder is picked: the node walks the whole tree under it, which takes
/// minutes for a large one. The screen owns the path and loads it (see StorageView).
struct DiskUsageSection: View {
    /// nil until the user picks a folder.
    let path: String?
    let state: LoadState<DiskUsage>
    let support: FeatureSupport
    /// Asks the screen to show another directory; nil stops waiting for the current one.
    let onOpen: (String?) -> Void

    var body: some View {
        Section {
            if let notice = support.localizedNotice {
                VersionNoticeRow(text: notice)
            } else {
                shortcuts
                if let path {
                    breadcrumb(path)
                    content(path)
                }
            }
        } header: {
            Text("Disk usage")
        } footer: {
            if support.supported {
                Text("Choose a folder to measure, then tap a folder to open it. Large folders such as /var can take minutes.")
            }
        }
    }

    @ViewBuilder
    private func content(_ path: String) -> some View {
        switch state {
        case .loading:
            HStack(spacing: 8) {
                ProgressView()
                Text("Measuring \(path)…").foregroundStyle(.secondary)
                Spacer()
                Button("Cancel") { onOpen(nil) }.buttonStyle(.borderless)
            }
        case .failed(let message):
            // Go's own words, e.g. that the folder is too large to measure in time.
            ErrorOrNoticeText(message: message)
        case .loaded(let usage, _, _):
            let rows = diskUsageRows(usage.entries, root: path)
            LabeledContent("Total", value: formatBytes(diskUsageTotal(usage.entries, root: path)))
            if rows.isEmpty {
                Text("Nothing here").font(.footnote).foregroundStyle(.secondary)
            }
            if usage.truncated {
                Text("Only the first \(rows.count) are shown").font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(rows) { row in
                if row.isDir {
                    Button { open(row.path) } label: { DiskUsageRowView(row: row) }
                        .buttonStyle(.plain)
                } else {
                    DiskUsageRowView(row: row)
                }
            }
        }
    }

    private var shortcuts: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(diskUsageShortcuts, id: \.self) { shortcut in
                    Button { open(shortcut) } label: {
                        Text(verbatim: shortcut).font(.caption.monospaced())
                    }
                    .buttonStyle(.bordered)
                    .tint(shortcut == path ? Color.accentColor : Color.secondary)
                }
            }
        }
    }

    /// "/ › var › lib": each step opens that directory.
    private func breadcrumb(_ path: String) -> some View {
        let crumbs = pathBreadcrumb(path)
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                ForEach(crumbs) { crumb in
                    if crumb.path != "/" {
                        Image(systemName: "chevron.right").font(.caption2).foregroundStyle(.tertiary)
                    }
                    Button { open(crumb.path) } label: {
                        Text(verbatim: crumb.name).font(.callout.monospaced())
                    }
                    .buttonStyle(.borderless)
                    .disabled(crumb.path == path)
                }
            }
        }
        .accessibilityLabel(Text("Path"))
    }

    private func open(_ target: String) {
        let next = normalizedPath(target)
        if next != path { onOpen(next) }
    }
}

private struct DiskUsageRowView: View {
    let row: DiskUsageRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: row.isDir ? "folder" : "doc")
                    .foregroundStyle(row.isDir ? Color.accentColor : Color.secondary)
                    .frame(width: 20)
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: formatBytes(row.size)).font(.caption).monospacedDigit().foregroundStyle(.secondary)
                if row.isDir {
                    Image(systemName: "chevron.right").font(.caption2).foregroundStyle(.tertiary)
                }
            }
            ProgressView(value: row.fraction)
            if !row.error.isEmpty {
                Text(verbatim: row.error).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .contentShape(Rectangle())
    }
}
