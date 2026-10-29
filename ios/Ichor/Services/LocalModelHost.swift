import Foundation
import Ichorgo
import UIKit

/// The Go core's LocalModel on iOS: the engine of native/llm (llama.cpp, on the GPU with
/// Metal). The model the user picked is loaded on the first question, kept a minute after the
/// last one, and freed when the app goes to the background or iOS warns about memory. One
/// question at a time (the Go core asks them so).
final class LocalModelHost: NSObject, IchorgoLocalModelProtocol, @unchecked Sendable {
    static let shared = LocalModelHost()

    private struct Options: Decodable {
        var contextTokens = 8192
        var answerTokens = 1024
        var temperature = 0.3
        var assistantPrefix = ""
    }

    private struct Turn: Decodable {
        let role: String
        let content: String
    }

    /// Passes the engine's pieces to the Go core's sink.
    private final class SinkBox {
        let sink: (any IchorgoLocalModelSinkProtocol)?
        init(_ sink: (any IchorgoLocalModelSinkProtocol)?) { self.sink = sink }
    }

    /// Held while a model is loaded, used or freed.
    private let modelLock = NSLock()
    private var handle: OpaquePointer?
    private var loadedID = ""
    private var loadedContext = 0
    private var unloadWork: DispatchWorkItem?

    /// Guards the two fields Cancel reads from another thread.
    private let stateLock = NSLock()
    private var running: OpaquePointer?
    private var cancelled = false

    private static let keepLoaded: TimeInterval = 60

    override private init() {
        super.init()
        let center = NotificationCenter.default
        center.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: nil) { _ in
            // Once an answer being written is done: off the main thread, which must not wait.
            DispatchQueue.global(qos: .utility).async { LocalModelHost.shared.unload() }
        }
        center.addObserver(forName: UIApplication.didReceiveMemoryWarningNotification, object: nil, queue: nil) { _ in
            LocalModelHost.shared.cancel()
            DispatchQueue.global(qos: .utility).async { LocalModelHost.shared.unload() }
        }
    }

    func generate(
        _ modelID: String?, optionsJSON: String?, system: String?, messagesJSON: String?,
        sink: (any IchorgoLocalModelSinkProtocol)?
    ) -> String {
        setCancelled(false)
        guard let modelID, let file = LocalModelStore.shared.file(for: modelID) else { return "not-downloaded" }
        let options = (try? JSONDecoder().decode(Options.self, from: Data((optionsJSON ?? "").utf8))) ?? Options()
        guard let turns = try? JSONDecoder().decode([Turn].self, from: Data((messagesJSON ?? "").utf8)) else {
            return "unreadable question"
        }

        modelLock.lock()
        defer { modelLock.unlock() }
        unloadWork?.cancel()

        if handle == nil || loadedID != modelID || loadedContext != options.contextTokens {
            freeLocked()
            var error = [CChar](repeating: 0, count: 256)
            let errorLen = error.count
            guard
                let loaded = ichor_llm_load(
                    file.path, Int32(options.contextTokens), Int32(Self.threads), 999, &error, errorLen)
            else {
                return String(cString: error)
            }
            handle = loaded
            loadedID = modelID
            loadedContext = options.contextTokens
        }
        guard let model = handle, !isCancelled() else { return "" }

        setRunning(model)
        defer {
            setRunning(nil)
            scheduleUnload()
        }

        let roles = ["system"] + turns.map(\.role)
        let contents = [system ?? ""] + turns.map(\.content)
        let box = SinkBox(sink)
        return withExtendedLifetime(box) {
            withCStrings(roles) { rolePointers in
                withCStrings(contents) { contentPointers in
                    var status = [CChar](repeating: 0, count: 256)
                    let statusLen = status.count
                    ichor_llm_generate(
                        model, rolePointers, contentPointers, roles.count, options.assistantPrefix,
                        Int32(options.answerTokens), Float(options.temperature),
                        { text, length, user in
                            guard let text, let user else { return }
                            let box = Unmanaged<SinkBox>.fromOpaque(user).takeUnretainedValue()
                            let bytes = UnsafeRawBufferPointer(start: text, count: length)
                            box.sink?.onPiece(String(decoding: bytes, as: UTF8.self))
                        },
                        Unmanaged.passUnretained(box).toOpaque(), &status, statusLen)
                    return String(cString: status)
                }
            }
        }
    }

    func cancel() {
        stateLock.lock()
        cancelled = true
        let model = running
        stateLock.unlock()
        if let model { ichor_llm_cancel(model) }
    }

    /// Frees the model once the answer being written, if any, is done.
    func unload() {
        modelLock.lock()
        defer { modelLock.unlock() }
        unloadWork?.cancel()
        freeLocked()
    }

    private func freeLocked() {
        if let handle { ichor_llm_free(handle) }
        handle = nil
        loadedID = ""
    }

    private func scheduleUnload() {
        let work = DispatchWorkItem { LocalModelHost.shared.unload() }
        unloadWork = work
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + Self.keepLoaded, execute: work)
    }

    private func setRunning(_ model: OpaquePointer?) {
        stateLock.lock()
        running = model
        stateLock.unlock()
    }

    private func setCancelled(_ value: Bool) {
        stateLock.lock()
        cancelled = value
        stateLock.unlock()
    }

    private func isCancelled() -> Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return cancelled
    }

    /// The performance cores, about: more threads only fight over them.
    private static var threads: Int { min(4, max(2, ProcessInfo.processInfo.activeProcessorCount - 2)) }
}

/// `strings` as C strings that live through `body`.
private func withCStrings<R>(_ strings: [String], _ body: (UnsafePointer<UnsafePointer<CChar>?>) -> R) -> R {
    let owned = strings.map { strdup($0) }
    defer { owned.forEach { free($0) } }
    let pointers = owned.map { UnsafePointer<CChar>($0) }
    return pointers.withUnsafeBufferPointer { body($0.baseAddress!) }
}
