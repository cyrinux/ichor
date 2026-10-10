import Foundation
import Ichorgo
import IchorCore

enum NetToolEvent: Sendable {
    /// A progress or output line, as it comes.
    case line(String)
    case done(NetToolResult)
    case failed(String)
}

/// Network checks from a node (see TalosClient for the conventions).
extension TalosClient {
    /// `tool` against `target` from `node`, in a privileged netshoot container (os:admin):
    /// output lines as they come, then the parsed result. Ending the stream cancels the run.
    func netTool(node: String, tool: NetTool, target: String, record: String = "", server: String = "",
                 count: Int = 0) -> AsyncStream<NetToolEvent> {
        let options = Self.netToolOptions(record: record, server: server, count: count)
        let trimmed = target.trimmingCharacters(in: .whitespacesAndNewlines)
        return Self.bridged { [config, context] continuation in
            let bridge = NetToolBridge(
                line: { continuation.yield(.line($0)) },
                done: { event in
                    continuation.yield(event)
                    continuation.finish()
                }
            )
            let run = IchorgoStartNodeNetTool(config, context, node, tool.rawValue, trimmed, options, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }

    private static func netToolOptions(record: String, server: String, count: Int) -> String {
        var options: [String: Any] = [:]
        if !record.isEmpty { options["record"] = record }
        let resolver = server.trimmingCharacters(in: .whitespaces)
        if !resolver.isEmpty { options["server"] = resolver }
        if count > 0 { options["count"] = count }
        guard let data = try? JSONSerialization.data(withJSONObject: options) else { return "{}" }
        return String(decoding: data, as: UTF8.self)
    }
}

private final class NetToolBridge: NSObject, IchorgoNetToolListenerProtocol, @unchecked Sendable {
    private let line: @Sendable (String) -> Void
    private let done: @Sendable (NetToolEvent) -> Void

    init(line: @escaping @Sendable (String) -> Void, done: @escaping @Sendable (NetToolEvent) -> Void) {
        self.line = line
        self.done = done
    }

    func onOutput(_ line: String?) { self.line(line ?? "") }

    func onDone(_ resultJSON: String?, errMessage: String?) {
        if let errMessage, !errMessage.isEmpty {
            done(.failed(errMessage))
            return
        }
        do {
            done(.done(try TalosJSON.decode(NetToolResult.self, from: resultJSON ?? "")))
        } catch {
            done(.failed(error.localizedDescription))
        }
    }
}
