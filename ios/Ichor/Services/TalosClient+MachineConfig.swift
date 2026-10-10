import Foundation
import Ichorgo
import IchorCore

/// A machine config being tried on a node. `events` finishes after `done`. Cancelling (or
/// dropping the consuming task) only stops following: the node reverts by itself at the end
/// of the timeout.
final class ConfigTryHandle: @unchecked Sendable {
    let events: AsyncStream<ConfigTryEvent>
    private let run: IchorgoConfigTryRun?

    fileprivate init(events: AsyncStream<ConfigTryEvent>, run: IchorgoConfigTryRun?) {
        self.events = events
        self.run = run
    }

    /// Makes the tried config permanent.
    func keep() { run?.keep() }

    /// Puts the previous config back now instead of waiting for the timeout.
    func revert() { run?.revert() }

    /// Stops following; the change is not kept.
    func cancel() { run?.cancel() }
}

/// Editing the machine config: schema, tree, field edits, preview and the try-mode apply (see
/// TalosClient for the conventions).
extension TalosClient {
    /// Makes the config schema of the node's Talos version ready (downloaded once per version);
    /// a schema that cannot be had is not an error.
    func machineConfigSchema(node: String) async throws -> ConfigSchemaStatus {
        try await Self.json { [config, context] in IchorgoMachineConfigSchemaPrepare(config, context, node, $0) }
    }

    /// The draft as a tree, with the schema of `talosVersion` when it is ready. Local.
    func describeMachineConfig(_ draft: String, talosVersion: String) async throws -> ConfigTree {
        try await Self.json { IchorgoMachineConfigDescribe(draft, talosVersion, $0) }
    }

    /// The draft with one field edit applied. Local: nothing is sent to the node.
    func editMachineConfig(_ draft: String, edit: ConfigEdit) async throws -> String {
        let editJSON = edit.json()
        return try await Self.run { IchorgoMachineConfigEdit(draft, editJSON, $0) }
    }

    /// What applying `draft` would change, validated by the node with a dry run (os:admin).
    /// `base` is the config the draft was made from, secrets hidden.
    func previewMachineConfig(node: String, base: String, draft: String) async throws -> ConfigPreview {
        try await Self.json { [config, context] in IchorgoMachineConfigPreview(config, context, node, base, draft, $0) }
    }

    /// Applies `draft` in try mode (os:admin): the node reverts after `timeoutSeconds` (one of
    /// `configTryTimeouts`) unless the handle's `keep` is called first.
    func tryMachineConfig(node: String, base: String, draft: String, timeoutSeconds: Int) -> ConfigTryHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: ConfigTryEvent.self)
        let bridge = ConfigTryBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done($0))
                continuation.finish()
            }
        )
        let run = IchorgoStartConfigTry(config, context, node, base, draft, timeoutSeconds, bridge)
        continuation.onTermination = { _ in
            run?.cancel()
            _ = bridge // keep the listener alive for the whole try
        }
        if run == nil {
            continuation.yield(.done(.failed(String(localized: "The config could not be applied."))))
            continuation.finish()
        }
        return ConfigTryHandle(events: stream, run: run)
    }
}

enum ConfigApplyEvent: Sendable {
    case progress(ConfigApplyProgress)
    /// nil on success.
    case done(error: String?)
}

extension TalosClient {
    /// Applies `draft` for good in `mode` (os:admin): now, at the next reboot, or now with a
    /// reboot. Ending the stream stops following the run; what was sent stays.
    func applyMachineConfig(node: String, base: String, draft: String, mode: ConfigApplyMode) -> AsyncStream<ConfigApplyEvent> {
        Self.bridged { [config, context] continuation in
            let bridge = ConfigApplyBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartConfigApply(config, context, node, base, draft, mode.rawValue, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

private final class ConfigApplyBridge: NSObject, IchorgoConfigApplyListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<ConfigApplyProgress>

    init(progress: @escaping @Sendable (ConfigApplyProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}

private final class ConfigTryBridge: NSObject, IchorgoConfigTryListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (ConfigTryProgress) -> Void
    private let done: @Sendable (ConfigTryOutcome) -> Void

    init(progress: @escaping @Sendable (ConfigTryProgress) -> Void, done: @escaping @Sendable (ConfigTryOutcome) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(ConfigTryProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ outcome: String?, errMessage: String?) {
        done(configTryOutcome(outcome: outcome ?? "", errMessage: errMessage ?? ""))
    }
}
