import Foundation
import Ichorgo
import IchorCore

extension TalosClient {
    /// Turns the startup timing on or off (always on in a debug build), the Go core's trace
    /// listener with it: none is set while off.
    static func setStartupTrace(_ enabled: Bool) {
        #if DEBUG
        let on = true
        #else
        let on = enabled
        #endif
        guard on != StartupTrace.isEnabled else { return }
        StartupTrace.setEnabled(on)
        if on {
            IchorgoSetTraceListener(GoTraceBridge.shared)
        } else {
            IchorgoSetTraceListener(nil)
        }
    }

    /// Applies the stored setting; call first at launch, so the trace sees the whole start.
    static func applyStoredStartupTrace() {
        setStartupTrace(UserDefaults.standard.bool(forKey: StartupTrace.defaultsKey))
    }
}

/// Forwards the Go core's trace steps to the same log (called on Go threads: no hop).
private final class GoTraceBridge: NSObject, IchorgoTraceListenerProtocol, @unchecked Sendable {
    static let shared = GoTraceBridge()

    func onTrace(_ json: String?) {
        StartupTrace.mark(StartupTrace.goStep(json: json ?? ""))
    }
}
