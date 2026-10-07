import SwiftUI
import UIKit
import IchorCore

/// A vulnerability report: the totals, filters (fixable only by default, severities), then each
/// image with its findings grouped by package; a finding opens in place (description, score,
/// advisory link). Export writes it through the core in a format and opens the share sheet.
struct ImageScanReportView: View {
    let report: ImageScanReport
    /// The core's own report ("" for the operator's, encoded again for the export).
    let reportJSON: String
    let appName: String

    @State private var fixableOnly = true
    @State private var hidden: Set<VulnSeverity> = []
    @State private var shared: SharedReport?
    @State private var exporting = false
    @State private var message: String?

    private var filter: VulnFilter {
        VulnFilter(fixableOnly: fixableOnly, severities: Set(VulnSeverity.allCases).subtracting(hidden))
    }

    var body: some View {
        List {
            Section {
                SeverityChips(summary: report.summary)
                Text(verbatim: reportSourceText(report)).font(.caption).foregroundStyle(.secondary)
            }
            Section {
                Toggle("Fixable only", isOn: $fixableOnly)
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 6) {
                        ForEach(VulnSeverity.allCases, id: \.self) { severity in
                            let on = !hidden.contains(severity)
                            Button {
                                if on { hidden.insert(severity) } else { hidden.remove(severity) }
                            } label: {
                                InfoChip(text: severity.label, color: on ? (severity.color ?? .accentColor) : nil)
                            }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(on ? .isSelected : [])
                        }
                    }
                }
            }
            ForEach(Array(report.images.enumerated()), id: \.offset) { _, image in
                ImageSection(image: image, filter: filter)
            }
        }
        .themedBackground()
        .navigationTitle("Vulnerability report")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    ForEach(ImageScanFormat.allCases) { format in
                        Button(format.label) { Task { await export(format) } }
                    }
                } label: {
                    if exporting { ProgressView() } else { Label("Export", systemImage: "square.and.arrow.up") }
                }
                .disabled(exporting)
            }
        }
        .sheet(item: $shared) { file in ActivityView(items: [file.url]) }
        .messageAlert($message)
    }

    private func export(_ format: ImageScanFormat) async {
        exporting = true
        defer { exporting = false }
        do {
            let json = try reportJSON.nonEmpty ?? String(decoding: JSONEncoder().encode(report), as: UTF8.self)
            let content = try await TalosClient.imageScanExport(json, format: format)
            shared = SharedReport(url: try writeSharedReport(content, name: imageScanFilename(app: appName, date: Date(), format: format)))
        } catch {
            message = String(localized: "Export failed: \(error.localizedDescription)")
        }
    }
}

/// One image: its name, OS and digest, pods, then its counts and findings, or why it was not scanned.
private struct ImageSection: View {
    let image: ScannedImage
    let filter: VulnFilter

    var body: some View {
        Section {
            let meta = [image.os, String(image.digest.prefix(19))].filter { !$0.isEmpty }
            if !meta.isEmpty { Text(verbatim: meta.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
            if !image.pods.isEmpty {
                Text("Pods: \(image.pods.joined(separator: ", "))").font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            if !image.error.isEmpty {
                Text(verbatim: imageScanErrorText(image.error)).font(.callout).foregroundStyle(.statusBad)
            } else {
                if image.summary.total > 0 { SeverityChips(summary: image.summary) }
                if image.summary.os > 0, !image.os.isEmpty {
                    Text("\(String(image.summary.os)) come from the OS packages (\(image.os)): a newer base image fixes those with a fix.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                let groups = image.byPackage(filter)
                if image.vulnerabilities.isEmpty {
                    Text("No known vulnerability.").note()
                } else if groups.isEmpty {
                    Text("None with these filters.").note()
                }
                ForEach(groups, id: \.package) { group in
                    PackageLine(first: group.vulns[0])
                    ForEach(group.vulns, id: \.key) { VulnLine(vuln: $0) }
                }
            }
        } header: {
            Text(verbatim: image.image.isEmpty ? image.ref : image.image).font(.footnote.monospaced()).textCase(nil)
        }
    }
}

private struct PackageLine: View {
    let first: ImageVuln

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            (Text(verbatim: first.package).font(.callout.monospaced()) + Text(verbatim: " \(first.installed)").font(.caption).foregroundStyle(.secondary))
                .lineLimit(1)
                .truncationMode(.middle)
            if first.class != "os-pkgs", !first.target.isEmpty {
                Text(verbatim: first.target).font(.caption2).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            }
        }
    }
}

/// Severity, ID, title and fixed version; expanded, the description, score and advisory link.
private struct VulnLine: View {
    let vuln: ImageVuln
    @State private var expanded = false

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 6) {
                if !vuln.description.isEmpty { Text(verbatim: vuln.description).font(.caption) }
                if vuln.score > 0 {
                    Text(verbatim: "CVSS \(String(format: "%.1f", vuln.score)) \(vuln.vector)").font(.caption2.monospaced()).foregroundStyle(.secondary)
                }
                if let url = URL(string: vuln.url), url.scheme == "https" || url.scheme == "http" {
                    Link("Open the advisory", destination: url).font(.caption)
                }
            }
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 8) {
                    Text(verbatim: vuln.level.label)
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(vuln.level.color ?? Color.secondary)
                        .frame(width: 58, alignment: .leading)
                    Text(verbatim: vuln.id).font(.caption.monospaced())
                    Spacer(minLength: 4)
                    Text(verbatim: vuln.fixable ? String(localized: "fixed in \(vuln.fixed)") : String(localized: "no fix yet"))
                        .font(.caption2)
                        .foregroundStyle(vuln.fixable ? AnyShapeStyle(Color.statusOK) : AnyShapeStyle(HierarchicalShapeStyle.secondary))
                        .lineLimit(1)
                }
                if !vuln.title.isEmpty {
                    Text(verbatim: vuln.title).font(.caption).foregroundStyle(.secondary).lineLimit(expanded ? nil : 1)
                }
            }
        }
    }
}

extension ImageScanFormat {
    var label: String {
        switch self {
        case .html: String(localized: "HTML report (printable)")
        case .sarif: "SARIF (GitHub, Defect Dojo)"
        case .cyclonedx: String(localized: "CycloneDX SBOM (Dependency-Track)")
        case .csv: String(localized: "CSV (spreadsheet)")
        case .json: "JSON"
        }
    }
}

private struct SharedReport: Identifiable {
    let url: URL
    var id: URL { url }
}

/// How long an exported report stays for the app it was shared to (an upload reads it late).
private let sharedReportLifetime: TimeInterval = 3600

/// Writes `content` to a file named `name` for the share sheet; exports older than an hour go first.
private func writeSharedReport(_ content: String, name: String) throws -> URL {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent("scans", isDirectory: true)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    let old = (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: [.contentModificationDateKey])) ?? []
    for file in old {
        let modified = (try? file.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate ?? .distantPast
        if Date().timeIntervalSince(modified) > sharedReportLifetime { try? FileManager.default.removeItem(at: file) }
    }
    let url = folder.appendingPathComponent(name)
    try content.write(to: url, atomically: true, encoding: .utf8)
    return url
}

/// The system share sheet for `items`.
private struct ActivityView: UIViewControllerRepresentable {
    let items: [Any]

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: items, applicationActivities: nil)
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}
