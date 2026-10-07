import CoreImage
import Ichorgo
import SwiftUI
import VisionKit

/// Live QR scanner (VisionKit). Generate a code on the desktop with
/// `qrencode -r ~/.talos/config -o talosconfig.png` (or `just qr`); a large config fits as
/// raw gzip: `gzip -9 < config | qrencode -8 -t ansiutf8`.
struct QRScannerView: View {
    let onScanned: (String) -> Void

    var body: some View {
        if DataScannerViewController.isSupported && DataScannerViewController.isAvailable {
            Scanner(onScanned: onScanned)
                .clipShape(RoundedRectangle(cornerRadius: 12))
        } else {
            ContentUnavailableView(
                "Scanner unavailable",
                systemImage: "qrcode.viewfinder",
                description: Text("Allow camera access in Settings, or use File / Paste.")
            )
        }
    }
}

private struct Scanner: UIViewControllerRepresentable {
    let onScanned: (String) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onScanned: onScanned) }

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
        private(set) var delivered = false

        init(onScanned: @escaping (String) -> Void) { self.onScanned = onScanned }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            guard !delivered else { return }
            for item in addedItems {
                if case .barcode(let barcode) = item, let payload = Self.text(of: barcode), !payload.isEmpty {
                    delivered = true
                    dataScanner.stopScanning()
                    onScanned(payload)
                    return
                }
            }
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
