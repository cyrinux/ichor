import SwiftUI
import IchorCore

/// An app's, a pod's or a workload's vulnerabilities (os:admin): the Trivy Operator's report on
/// its pods, read when the section shows (nothing runs in the cluster for it), or a scan started
/// from here with a Job of the scanner picked in Settings, which goes on when the screen closes
/// (ImageScanJob). A workload is scanned by its spec's image refs (`target.byRefs`).
struct AppScanSection: View {
    let target: ImageScanTarget
    /// Why what to scan could not be read (a workload's images), shown first.
    let targetError: String?

    init(app: InventoryApp) {
        self.init(target: ImageScanTarget(app: app))
    }

    init(target: ImageScanTarget, targetError: String? = nil) {
        self.target = target
        self.targetError = targetError
    }

    @Environment(AppModel.self) private var model
    @State private var operatorState: LoadState<OperatorReports> = .loading
    /// The newest report kept on the phone for this target.
    @State private var saved: SavedScan?
    @AppStorage(ImageScannerKeys.selected) private var scanner = ImageScanner.defaultScanner

    private var job: ImageScanJob { ImageScanJob.shared }
    private var scan: ImageScanState? { job.scan(context: model.activeContext, appID: target.id) }
    private var busyElsewhere: Bool { job.busy(otherThan: model.activeContext, appID: target.id) }

    /// The Trivy Operator's report, shown when the target has no scan of its own.
    private var operatorReport: ImageScanReport? {
        if case .loaded(let reports, _, _) = operatorState, !reports.report.images.isEmpty { return reports.report }
        return nil
    }

    /// Reads the kept reports again when the target, the cluster or the scan's state changes.
    private var savedKey: String {
        "\(model.activeContext)|\(target.id)|\(scan?.running == true)|\(scan?.report?.finished ?? 0)"
    }

    var body: some View {
        Section("Vulnerabilities") {
            if let scan, scan.running {
                ScanProgressLines(scan: scan) { job.stop() }
            } else {
                if let targetError {
                    Text(verbatim: targetError).note()
                }
                if let error = scan?.error {
                    Text("The scan failed: \(imageScanErrorText(error))").font(.callout).foregroundStyle(.statusBad)
                }
                if case .failed(let message) = operatorState {
                    Text("Could not read the Trivy Operator's reports: \(message)").note()
                }
                // This session's scan first, then the newest kept on the phone, then the operator's.
                let current = scan?.usableReport
                let lastSaved = current == nil ? saved : nil
                if let current, let scan {
                    LastScanLines(finished: current.finished, summary: current.summary)
                    NewFindingsLine(summary: current.newSummary, hasPrevious: current.hasPrevious)
                    if current.failed > 0 {
                        Text("Images not scanned: \(String(current.failed))").font(.caption).foregroundStyle(
                            .statusWarn)
                    }
                    NavigationLink {
                        ImageScanReportView(report: current, reportJSON: scan.reportJSON, appName: target.name)
                    } label: {
                        Label("View", systemImage: "doc.text.magnifyingglass")
                    }
                } else if let lastSaved {
                    LastScanLines(finished: lastSaved.finished, summary: lastSaved.summary)
                    NewFindingsLine(summary: lastSaved.newSummary, hasPrevious: false)
                    NavigationLink {
                        SavedScanReportView(saved: lastSaved, appName: target.name)
                    } label: {
                        Label("View", systemImage: "doc.text.magnifyingglass")
                    }
                } else if let report = operatorReport {
                    SeverityChips(summary: report.summary)
                    Text(verbatim: reportSourceText(report)).font(.caption).foregroundStyle(.secondary)
                    NavigationLink {
                        ImageScanReportView(report: report, reportJSON: "", appName: target.name)
                    } label: {
                        Label("Open report", systemImage: "doc.text.magnifyingglass")
                    }
                } else if !target.byRefs {
                    Text(
                        "Checks the images these pods run, by digest, for known vulnerabilities with \(scanner.label). It runs as a Job in the \(imageScanNamespace) namespace, which deletes itself afterwards."
                    ).note()
                }
                if target.byRefs {
                    Text(
                        "Scanned by the image references in this workload's spec: the findings are for those tags, not necessarily what runs right now."
                    ).note()
                }
                if busyElsewhere {
                    Text("Another app's scan is running: one at a time.").note()
                }
                Button {
                    guard let client = model.client else { return }
                    job.start(
                        client: client, context: model.activeContext, appID: target.id, pods: target.pods,
                        images: target.images)
                } label: {
                    Label(
                        current != nil || lastSaved != nil
                            ? LocalizedStringKey("Scan again") : LocalizedStringKey("Scan images"),
                        systemImage: "checkmark.shield")
                }
                .disabled(busyElsewhere || target.isEmpty)
                NavigationLink {
                    SavedScansView(cluster: model.activeContext)
                } label: {
                    Label("All scan reports", systemImage: "tray.full")
                }
            }
        }
        .task(id: savedKey) {
            let history = (try? await TalosClient.imageScanHistory(cluster: model.activeContext)) ?? []
            saved = history.last(of: target.id)
        }
        .task(id: target) {
            operatorState = .loading
            guard let client = model.client else { return }
            let pods = target.pods
            guard !pods.isEmpty else {
                operatorState = .loaded(OperatorReports(), at: Date())
                return
            }
            operatorState = await .from { try await client.imageScanOperatorReports(pods: pods) }
        }
    }
}

/// The running scan's phase, a progress bar (by image while scanning) and Stop.
struct ScanProgressLines: View {
    let scan: ImageScanState
    let onStop: () -> Void

    var body: some View {
        let progress = scan.progress
        Text(
            verbatim: scan.stopping ? String(localized: "Stopping…") : phaseText(progress, scanner: scan.scanner.label))
        if let progress, progress.phase == ImageScanPhase.scanning, progress.steps > 0 {
            ProgressView(value: Double(max(progress.step - 1, 0)), total: Double(progress.steps))
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
        if !scan.stopping {
            Button("Stop", role: .destructive, action: onStop)
        }
    }

    private func phaseText(_ p: ImageScanProgress?, scanner: String) -> String {
        imageScanPhaseText(p, scanner: scanner)
    }
}

/// "Last scan 3 minutes ago" and the severities it found.
private struct LastScanLines: View {
    let finished: Int64
    let summary: VulnSummary

    var body: some View {
        Text("Last scan \(relativeTime(finished))").font(.caption).foregroundStyle(.secondary)
        SeverityChips(summary: summary)
    }
}

/// What is new since the last scan of the same images: "3 new" and their severities, or
/// "Nothing new since the last scan" when `hasPrevious` says there was one to compare with.
struct NewFindingsLine: View {
    let summary: VulnSummary
    let hasPrevious: Bool

    var body: some View {
        if summary.total > 0 {
            HStack(spacing: 6) {
                Text("\(String(summary.total)) new").font(.caption.weight(.semibold)).foregroundStyle(.statusWarn)
                ForEach(VulnSeverity.allCases.filter { summary.count($0) > 0 }, id: \.self) { severity in
                    Tag("\(severity.label) \(summary.count(severity))", color: severity.color)
                }
            }
            .lineLimit(1)
            .minimumScaleFactor(0.7)
        } else if hasPrevious {
            Text("Nothing new since the last scan").font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// "Critical 2  High 5  …": one chip per severity, muted when none (Unknown only when found).
struct SeverityChips: View {
    let summary: VulnSummary

    var body: some View {
        HStack(spacing: 6) {
            ForEach(VulnSeverity.allCases, id: \.self) { severity in
                let count = summary.count(severity)
                if severity != .unknown || count > 0 {
                    Tag("\(severity.label) \(count)", color: count > 0 ? severity.color : nil)
                }
            }
        }
        .lineLimit(1)
        .minimumScaleFactor(0.7)
    }
}

extension VulnSeverity {
    var label: String {
        switch self {
        case .critical: String(localized: "Critical")
        case .high: String(localized: "High")
        case .medium: String(localized: "Medium")
        case .low: String(localized: "Low")
        case .unknown: String(localized: "Unknown")
        }
    }

    /// Critical and high in the "bad" colour (high warmer), medium "warn", the rest neutral.
    var color: Color? {
        switch self {
        case .critical: Color.statusBad
        case .high: Color.orange
        case .medium: Color.statusWarn
        case .low, .unknown: nil
        }
    }
}

/// "Trivy 0.75.0 · 3 minutes ago", or "From the Trivy Operator · 2 days ago".
func reportSourceText(_ report: ImageScanReport) -> String {
    let when = relativeTime(report.madeAt)
    if report.source == ImageScanSource.operatorReports {
        return String(localized: "From the Trivy Operator · \(when)")
    }
    return "\(report.scanner) · \(when)"
}

/// A core message in the user's language when it is one of the scan's own.
func imageScanErrorText(_ message: String) -> String {
    switch message {
    case imageScanNotScanned: String(localized: "Not scanned.")
    case imageScanTimedOut: String(localized: "The scan took too long and was stopped.")
    default: message
    }
}
