import SwiftUI
import IchorCore

/// An app's vulnerabilities (os:admin): the Trivy Operator's report, read when the sheet opens
/// (nothing runs in the cluster for it), or a scan started from here with a Trivy Job, which
/// goes on when the sheet closes (ImageScanJob).
struct AppScanSection: View {
    let app: InventoryApp

    @Environment(AppModel.self) private var model
    @State private var operatorState: LoadState<OperatorReports> = .loading

    private var job: ImageScanJob { ImageScanJob.shared }
    private var scan: ImageScanState? { job.scan(context: model.activeContext, appID: app.id) }
    private var busyElsewhere: Bool { job.busy(otherThan: model.activeContext, appID: app.id) }

    /// The report to show: the app's last scan when it scanned something, else the operator's.
    private var shown: (report: ImageScanReport, json: String)? {
        if let scan, let report = scan.usableReport { return (report, scan.reportJSON) }
        if case .loaded(let reports, _, _) = operatorState, !reports.report.images.isEmpty { return (reports.report, "") }
        return nil
    }

    var body: some View {
        Section("Vulnerabilities") {
            if let scan, scan.running {
                ScanProgressLines(scan: scan) { job.stop() }
            } else {
                if let error = scan?.error {
                    Text("The scan failed: \(imageScanErrorText(error))").font(.callout).foregroundStyle(.statusBad)
                }
                if case .failed(let message) = operatorState {
                    Text("Could not read the Trivy Operator's reports: \(message)").note()
                }
                if let shown {
                    SeverityChips(summary: shown.report.summary)
                    Text(verbatim: reportSourceText(shown.report)).font(.caption).foregroundStyle(.secondary)
                    if shown.report.failed > 0 {
                        Text("Images not scanned: \(String(shown.report.failed))").font(.caption).foregroundStyle(.statusWarn)
                    }
                    NavigationLink {
                        ImageScanReportView(report: shown.report, reportJSON: shown.json, appName: app.name)
                    } label: {
                        Label("Open report", systemImage: "doc.text.magnifyingglass")
                    }
                } else {
                    Text("Checks the images these pods run, by digest, for known vulnerabilities with Trivy. It runs as a Job in the \(imageScanNamespace) namespace, which deletes itself afterwards.").note()
                }
                if busyElsewhere {
                    Text("Another app's scan is running: one at a time.").note()
                }
                Button {
                    guard let client = model.client else { return }
                    job.start(client: client, context: model.activeContext, appID: app.id, pods: app.routePods)
                } label: {
                    Label(scan?.usableReport != nil ? LocalizedStringKey("Scan again") : LocalizedStringKey("Scan images"),
                          systemImage: "checkmark.shield")
                }
                .disabled(busyElsewhere || app.pods.isEmpty)
            }
        }
        .task(id: app) {
            operatorState = .loading
            guard let client = model.client else { return }
            let pods = app.routePods
            guard !pods.isEmpty else {
                operatorState = .loaded(OperatorReports(), at: Date())
                return
            }
            operatorState = await .from { try await client.imageScanOperatorReports(pods: pods) }
        }
    }
}

/// The running scan's phase, a progress bar (by image while scanning) and Stop.
private struct ScanProgressLines: View {
    let scan: ImageScanState
    let onStop: () -> Void

    var body: some View {
        let progress = scan.progress
        Text(verbatim: scan.stopping ? String(localized: "Stopping…") : phaseText(progress))
        if let progress, progress.phase == ImageScanPhase.scanning, progress.steps > 0 {
            ProgressView(value: Double(max(progress.step - 1, 0)), total: Double(progress.steps))
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
        if !scan.stopping {
            Button("Stop", role: .destructive, action: onStop)
        }
    }

    private func phaseText(_ p: ImageScanProgress?) -> String {
        switch p?.phase {
        case ImageScanPhase.starting:
            guard let p, !p.message.isEmpty else { return String(localized: "Starting Trivy…") }
            return String(localized: "Starting Trivy (\(p.message))…")
        case ImageScanPhase.database:
            return String(localized: "Downloading the vulnerability database…")
        case ImageScanPhase.scanning:
            let p = p ?? ImageScanProgress()
            let image = String(p.image.split(separator: "/").last ?? "")
            return String(localized: "Scanning \(String(p.step)) of \(String(p.steps)): \(image)")
        case ImageScanPhase.cleaning:
            return String(localized: "Cleaning up…")
        default:
            return String(localized: "Reading the pods and their images…")
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
                    InfoChip(text: "\(severity.label) \(count)", color: count > 0 ? severity.color : nil)
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
