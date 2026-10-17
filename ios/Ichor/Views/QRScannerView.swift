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
                .sensoryFeedback(.success, trigger: parts.successes)
                .sensoryFeedback(.warning, trigger: parts.warnings)
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
    /// Why the set restarted or failed, or why a one-code scan was ignored, until the next new part.
    private(set) var message: String?
    /// Bumped on each new part and on completion, for the success haptic.
    private(set) var successes = 0
    /// Bumped when the set restarts or fails, for the warning haptic.
    private(set) var warnings = 0
    /// Set by the scanner's coordinator: reads the codes already in frame again after Start over.
    @ObservationIgnored var onStartOver: (() -> Void)?

    func add(_ text: String) -> QRPartOutcome {
        let outcome = collector.add(text, part: Self.part, join: Self.join)
        switch outcome {
        case .added:
            successes += 1
            message = nil
        case .complete:
            successes += 1
            message = nil
        case .restarted:
            warnings += 1
            message = String(localized: "This code belongs to another set: started again from it.")
        case .failed(let error):
            // A set that does not join is dropped (the collector is empty again): scanning goes on.
            warnings += 1
            message = error.message
        case .notPartWhileCollecting:
            message = String(localized: "Finish this set or start over first.")
        case .notPart, .duplicate:
            break
        }
        return outcome
    }

    func startOver() {
        collector.reset()
        message = nil
        onStartOver?()
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
        context.coordinator.scanner = scanner
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
        weak var scanner: DataScannerViewController?
        /// The codes in frame, as the last delegate call listed them.
        private var inFrame: [RecognizedItem] = []
        /// Codes in frame when a set failed: not read again (which would hide the failure)
        /// until they leave the frame or Start over.
        private var muted: Set<RecognizedItem.ID> = []

        init(onScanned: @escaping (String) -> Void, parts: QRPartsProgress) {
            self.onScanned = onScanned
            self.parts = parts
            super.init()
            parts.onStartOver = { [weak self] in self?.readAgain() }
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            inFrame = allItems
            feed(addedItems, from: dataScanner)
        }

        // A code held in frame is reported as updated, not added: a part first read
        // unclearly, or one in frame again after Start over, is still collected
        // (duplicates are ignored).
        func dataScanner(_ dataScanner: DataScannerViewController, didUpdate updatedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            inFrame = allItems
            feed(updatedItems, from: dataScanner)
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didRemove removedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            inFrame = allItems
            muted.subtract(removedItems.map(\.id))
        }

        /// After Start over: the codes already in frame count again.
        private func readAgain() {
            muted = []
            guard let scanner else { return }
            feed(inFrame, from: scanner)
        }

        private func feed(_ items: [RecognizedItem], from dataScanner: DataScannerViewController) {
            guard !delivered else { return }
            for item in items where !muted.contains(item.id) {
                guard case .barcode(let barcode) = item, let payload = Self.text(of: barcode), !payload.isEmpty else { continue }
                // A part of a multi-part set waits for the others; a one-code scan imports
                // unless a set is being collected (the core then answers notPartWhileCollecting).
                switch parts.add(payload) {
                case .notPart:
                    deliver(payload, from: dataScanner)
                    return
                case .complete(let text):
                    deliver(text, from: dataScanner)
                    return
                case .failed:
                    if !parts.collector.isCollecting {
                        muted = Set(inFrame.map(\.id))
                        return
                    }
                case .added, .duplicate, .restarted, .notPartWhileCollecting:
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
