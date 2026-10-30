import Foundation
#if canImport(os)
    import os
#endif

/// Where a cold start's time goes, in the unified log (Console, category `startup`): one line
/// per step with the time since the process started and since the previous step, the Go
/// core's steps (config parse, session, first node, all nodes) in the same stream, and a
/// signpost interval per Go call for Instruments.
///
/// The app's steps also go to the core's redacted diagnostics log through `setRingSink`,
/// whatever the setting (the core writes its own steps there too), so an exported diagnostics
/// zip has the last start's steps.
///
/// The unified log is off unless the "Startup timing" setting is on (UserDefaults
/// `startupTrace`) or the build is a debug one. The lines carry step names, call names, counts
/// and durations only: never a context name, an endpoint or an address (the privacy mask does
/// not cover the log).
public enum StartupTrace {
    /// UserDefaults key of the setting.
    public static let defaultsKey = "startupTrace"

    private static let state = TraceState()

    #if canImport(os)
        private static let logger = Logger(
            subsystem: Bundle.main.bundleIdentifier ?? "name.levis.ichor", category: "startup")
        private static let signposter = OSSignposter(logger: logger)
    #endif

    public static var isEnabled: Bool { state.enabled }

    /// Turns the trace on or off (the app also sets or removes the Go listener, see TalosClient).
    public static func setEnabled(_ on: Bool) { state.enabled = on }

    /// Where the app's steps go besides the unified log: (step, ms since the process started,
    /// ms since the previous step). The app points it at the core's diagnostics log.
    public typealias RingSink = @Sendable (_ step: String, _ sinceStart: Int, _ delta: Int) -> Void

    /// Sets (or removes) the diagnostics log sink; call first at launch.
    public static func setRingSink(_ sink: RingSink?) { state.sink = sink }

    /// Writes the app's `step` now: to the diagnostics log always, to the unified log when the
    /// trace is on.
    public static func mark(_ step: String) { mark(step, ring: true) }

    /// A Go core step: to the unified log only, as the core writes its own steps to the
    /// diagnostics log.
    public static func markGo(_ step: String) { mark(step, ring: false) }

    private static func mark(_ step: String, ring: Bool) {
        let sink = ring ? state.sink : nil
        let on = state.enabled
        guard sink != nil || on else { return }
        let (sinceStart, delta) = state.tick()
        sink?(step, sinceStart, delta)
        if on { log(line(step: step, sinceStart: sinceStart, delta: delta)) }
    }

    /// `mark` the first time `key` is seen in this process only: a step of the cold start that
    /// runs again later (a refresh, a background check) logs once. `detail` follows the key.
    public static func markOnce(_ key: String, detail: String = "") {
        guard state.firstTime(key) else { return }
        mark(detail.isEmpty ? key : "\(key) \(detail)")
    }

    /// A Go call being timed (`begin` … `end`); nil while the trace is off.
    public struct Span {
        let name: String
        let start: Date
        #if canImport(os)
            let signpost: OSSignpostIntervalState
        #endif
    }

    /// Starts timing the Go call `name`; nil (and nothing logged) while the trace is off.
    public static func begin(_ name: String) -> Span? {
        guard state.enabled else { return nil }
        #if canImport(os)
            let signpost = signposter.beginInterval(
                "go call", id: signposter.makeSignpostID(), "\(name, privacy: .public)")
            return Span(name: name, start: Date(), signpost: signpost)
        #else
            return Span(name: name, start: Date())
        #endif
    }

    /// Ends `span`: logs "call <name> <ms>ms" to the unified log and closes its signpost interval.
    public static func end(_ span: Span?) {
        guard let span else { return }
        #if canImport(os)
            signposter.endInterval("go call", span.signpost)
        #endif
        // The unified log only: a diagnostics line per Go call would crowd out the log.
        mark(callStep(name: span.name, ms: Int(Date().timeIntervalSince(span.start) * 1000)), ring: false)
    }

    /// "<step> +<ms since process start> (Δ<ms since the previous step>)", as on Android.
    public static func line(step: String, sinceStart: Int, delta: Int) -> String {
        "\(step) +\(sinceStart)ms (Δ\(delta)ms)"
    }

    /// "call overview() 812ms": a Go call's name and duration.
    public static func callStep(name: String, ms: Int) -> String {
        "call \(name) \(ms)ms"
    }

    /// A Go trace step ({"step","ms","at","count","unreachable"}) as a step name for `mark`:
    /// "go:overview.all_nodes 8003ms count=8 unreachable=1". Anything else is kept as it came.
    public static func goStep(json: String) -> String {
        struct Step: Decodable {
            let step: String
            let ms: Int
            let count: Int?
            let unreachable: Int?
        }
        guard let data = json.data(using: .utf8), let s = try? JSONDecoder().decode(Step.self, from: data) else {
            return "go:\(json)"
        }
        var out = "go:\(s.step) \(s.ms)ms"
        if let count = s.count, count > 0 { out += " count=\(count)" }
        if let unreachable = s.unreachable, unreachable > 0 { out += " unreachable=\(unreachable)" }
        return out
    }

    private static func log(_ line: String) {
        #if canImport(os)
            logger.info("\(line, privacy: .public)")
        #endif
    }
}

/// The trace's mutable state, behind a lock: Go calls it from its own threads.
private final class TraceState: Sendable {
    private struct State {
        var on = false
        var ringSink: StartupTrace.RingSink?
        var previous = 0
        var marked: Set<String> = []
    }

    private let state = Locked(State())
    private let processStart = TraceState.processStartTime() ?? Date().timeIntervalSince1970

    var enabled: Bool {
        get { state.withLock { $0.on } }
        set { state.withLock { $0.on = newValue } }
    }

    var sink: StartupTrace.RingSink? {
        get { state.withLock { $0.ringSink } }
        set { state.withLock { $0.ringSink = newValue } }
    }

    /// Milliseconds since the process started and since the previous tick.
    func tick() -> (Int, Int) {
        // The clock is read under the lock, so two ticks never see their times out of order.
        state.withLock { [processStart] s in
            let now = Int((Date().timeIntervalSince1970 - processStart) * 1000)
            let delta = now - s.previous
            s.previous = now
            return (now, delta)
        }
    }

    func firstTime(_ key: String) -> Bool {
        state.withLock { $0.marked.insert(key).inserted }
    }

    /// When the kernel started this process (wall clock), so the first step counts the launch.
    private static func processStartTime() -> Double? {
        // Not on watchOS: the watch app never traces, and its SDK may not offer kinfo_proc.
        #if canImport(Darwin) && !os(watchOS)
            var info = kinfo_proc()
            var size = MemoryLayout<kinfo_proc>.stride
            var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
            guard sysctl(&mib, u_int(mib.count), &info, &size, nil, 0) == 0 else { return nil }
            let start = info.kp_proc.p_starttime
            return Double(start.tv_sec) + Double(start.tv_usec) / 1_000_000
        #else
            return nil
        #endif
    }
}
