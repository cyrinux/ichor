import CoreImage
import IchorCore
import Ichorgo
import SwiftUI
import VisionKit

/// Live QR scanner (VisionKit). Generate a code on the desktop with
/// `qrencode -r ~/.talos/config -o talosconfig.png` (or `just qr`); a large config fits as
/// raw gzip: `gzip -9 < config | qrencode -8 -t ansiutf8`, a larger one over several codes
/// (`just qr-parts`), scanned in any order.
struct QRScannerView: View {
    let onScanned: (String) -> Void
    @State private var parts = QRPartsProgress()

    var body: some View {
        if DataScannerViewController.isSupported && DataScannerViewController.isAvailable {
            Scanner(onScanned: onScanned, parts: parts)
                .clipShape(RoundedRectangle(cornerRadius: 12))
                .overlay(alignment: .bottom) {
                    if parts.collector.isCollecting || parts.message != nil {
                        QRPartsOverlay(parts: parts).padding()
                    }
                }
                .sensoryFeedback(.success, trigger: parts.newParts)
        } else {
            ContentUnavailableView(
                "Scanner unavailable",
                systemImage: "qrcode.viewfinder",
                description: Text("Allow camera access in Settings, or use File / Paste.")
            )
        }
    }
}

/// The parts of a multi-part import collected so far, shared by the scanner's coordinator and
/// the overlay. Never logs a part: each chunk is a piece of a credential.
@Observable
final class QRPartsProgress {
    private(set) var collector = QRPartCollector()
    /// Why the set restarted or failed, until the next new part.
    private(set) var message: String?
    /// Bumped on each new part, for the haptic.
    private(set) var newParts = 0

    func add(_ text: String) -> QRPartOutcome {
        let outcome = collector.add(text, part: Self.part, join: Self.join)
        switch outcome {
        case .added:
            newParts += 1
            message = nil
        case .restarted:
            newParts += 1
            message = String(localized: "This code belongs to another set: started again from it.")
        case .failed(let error):
            message = error.message
        case .complete:
            message = nil
        case .notPart, .duplicate:
            break
        }
        return outcome
    }

    func startOver() {
        collector.reset()
        message = nil
    }

    private static func part(_ text: String) throws -> String {
        var error: NSError?
        let json = IchorgoQRPart(text, &error)
        if let error { throw TalosError(message: error.localizedDescription) }
        return json
    }

    private static func join(_ json: String) throws -> String {
        var error: NSError?
        let text = IchorgoQRPartsJoin(json, &error)
        if let error { throw TalosError(message: error.localizedDescription) }
        return text
    }
}

extension QRPartsError {
    var message: String {
        switch self {
        case .mismatch: String(localized: "These codes come from different sets. Start over and scan one set.")
        case .missing: String(localized: "Some codes are missing. Start over and scan them all.")
        case .invalid: String(localized: "These codes do not make a config. Start over and scan them again.")
        }
    }
}

/// The progress ring "k of n", the indexes still to scan and a Start over button.
private struct QRPartsOverlay: View {
    let parts: QRPartsProgress

    var body: some View {
        let collector = parts.collector
        VStack(spacing: 8) {
            if collector.isCollecting {
                HStack(spacing: 12) {
                    ZStack {
                        Circle().stroke(.quaternary, lineWidth: 5)
                        Circle()
                            .trim(from: 0, to: CGFloat(collector.received) / CGFloat(max(collector.total, 1)))
                            .stroke(.tint, style: StrokeStyle(lineWidth: 5, lineCap: .round))
                            .rotationEffect(.degrees(-90))
                    }
                    .frame(width: 32, height: 32)
                    .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("\(collector.received) of \(collector.total) codes scanned").font(.headline)
                        Text("Missing: \(collector.missing.map(String.init).joined(separator: ", "))")
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
            if let message = parts.message {
                Text(message).font(.footnote).multilineTextAlignment(.center)
            }
            Button("Start over") { parts.startOver() }
                .buttonStyle(.bordered)
        }
        .padding()
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12))
    }
}

private struct Scanner: UIViewControllerRepresentable {
    let onScanned: (String) -> Void
    let parts: QRPartsProgress

    func makeCoordinator() -> Coordinator { Coordinator(onScanned: onScanned, parts: parts) }

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])],
            qualityLevel: .accurate,
            isHighlightingEnabled: true
        )
        scanner.delegate = context.coordinator
        return scanner
    }

    // Start once the view is in the hierarchy; starting in make… can fail silently.
    func updateUIViewController(_ controller: DataScannerViewController, context: Context) {
        if !controller.isScanning && !context.coordinator.delivered {
            try? controller.startScanning()
        }
    }

    static func dismantleUIViewController(_ controller: DataScannerViewController, coordinator: Coordinator) {
        controller.stopScanning()
    }

    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        private let onScanned: (String) -> Void
        private let parts: QRPartsProgress
        private(set) var delivered = false

        init(onScanned: @escaping (String) -> Void, parts: QRPartsProgress) {
            self.onScanned = onScanned
            self.parts = parts
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            guard !delivered else { return }
            for item in addedItems {
                guard case .barcode(let barcode) = item, let payload = Self.text(of: barcode), !payload.isEmpty else { continue }
                // A part of a multi-part set waits for the others; anything else is a one-code scan.
                switch parts.add(payload) {
                case .notPart:
                    deliver(payload, from: dataScanner)
                    return
                case .complete(let text):
                    deliver(text, from: dataScanner)
                    return
                case .added, .duplicate, .restarted, .failed:
                    continue
                }
            }
        }

        private func deliver(_ text: String, from dataScanner: DataScannerViewController) {
            delivered = true
            dataScanner.stopScanning()
            onScanned(text)
        }

        /// The decoded text, or for a binary (gzip) code, which the decoded text mangles,
        /// its "ichor-config:" form read off the symbol's codewords.
        private static func text(of barcode: RecognizedItem.Barcode) -> String? {
            guard let qr = barcode.observation.barcodeDescriptor as? CIQRCodeDescriptor else {
                return barcode.payloadStringValue
            }
            var error: NSError?
            let text = IchorgoQRCodeText(barcode.payloadStringValue ?? "", qr.errorCorrectedPayload, qr.symbolVersion, &error)
            return error == nil ? text : barcode.payloadStringValue
        }
    }
}
