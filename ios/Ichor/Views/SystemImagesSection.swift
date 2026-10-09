import SwiftUI
import IchorCore

/// The images Talos runs on a node outside of any app (kubelet, etcd, the control plane…), read
/// with os:admin (the machine config), and their vulnerability scan: the same Trivy Job as an
/// app's, kept by ImageScanJob, offered to a role that reaches the Kubernetes API.
struct SystemImagesSection: View {
    let node: String
    let hostname: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[SystemImage]> = .loading

    private var job: ImageScanJob { ImageScanJob.shared }
    private var scanID: String { systemImagesScanID(node: node) }
    private var scan: ImageScanState? { job.scan(context: model.activeContext, appID: scanID) }
    private var busyElsewhere: Bool { job.busy(otherThan: model.activeContext, appID: scanID) }

    var body: some View {
        Section("System images") {
            switch state {
            case .loading:
                Text("Reading the node's system images…").note()
            case .failed(let message):
                Text("System images need the os:admin role: \(message)").note()
            case .loaded(let images, _, _):
                ForEach(images) { SystemImageRow(image: $0) }
                if model.allows(.workloads), !images.isEmpty {
                    scanControls(images)
                }
            }
        }
        .task(id: node) {
            guard let client = model.client else { return }
            state = await .from { try await client.systemImages(node: node) }
        }
    }

    @ViewBuilder
    private func scanControls(_ images: [SystemImage]) -> some View {
        if let scan, scan.running {
            ScanProgressLines(scan: scan) { job.stop() }
        } else {
            if let error = scan?.error {
                Text("The scan failed: \(imageScanErrorText(error))").font(.callout).foregroundStyle(.statusBad)
            }
            if let scan, let report = scan.usableReport {
                SeverityChips(summary: report.summary)
                Text(verbatim: reportSourceText(report)).font(.caption).foregroundStyle(.secondary)
                NavigationLink {
                    ImageScanReportView(report: report, reportJSON: scan.reportJSON, appName: "\(hostname)-system")
                } label: {
                    Label("Open report", systemImage: "doc.text.magnifyingglass")
                }
            } else {
                Text("Checks the images Talos runs on this node (kubelet, etcd, the control plane), by digest, for known vulnerabilities with Trivy. It runs as a Job in the \(imageScanNamespace) namespace, which deletes itself afterwards.").note()
            }
            if busyElsewhere {
                Text("Another app's scan is running: one at a time.").note()
            }
            Button {
                guard let client = model.client else { return }
                job.start(client: client, context: model.activeContext, appID: scanID, pods: [], images: images.map(\.ref))
            } label: {
                Label(scan?.usableReport != nil ? LocalizedStringKey("Scan again") : LocalizedStringKey("Scan system images"),
                      systemImage: "checkmark.shield")
            }
            .disabled(busyElsewhere)
        }
    }
}

/// "kubelet  kubelet:v1.34.1  sha256:0123456789ab".
private struct SystemImageRow: View {
    let image: SystemImage

    var body: some View {
        HStack(spacing: 8) {
            Text(verbatim: image.role).font(.caption.weight(.semibold))
            Text(verbatim: String(image.image.split(separator: "/").last ?? ""))
                .font(.caption.monospaced())
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 0)
            if !image.digest.isEmpty {
                Text(verbatim: shortDigest(image.digest)).font(.caption2.monospaced()).foregroundStyle(.secondary)
            }
        }
    }
}
