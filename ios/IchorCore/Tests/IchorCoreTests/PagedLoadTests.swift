import Foundation
import XCTest
@testable import IchorCore

/// Runs async code from a synchronous test: test-linux.sh lists plain test methods only.
func runBlocking<T>(_ body: @escaping @Sendable () async throws -> T) throws -> T {
    let box = Locked<Result<T, Error>?>(nil)
    let done = DispatchSemaphore(value: 0)
    Task {
        do { box.set(.success(try await body())) } catch { box.set(.failure(error)) }
        done.signal()
    }
    done.wait()
    return try box.get()!.get()
}

struct PageError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

/// A list of `total` rows served `size` at a time; the token is the next row's index.
final class PageServer: Sendable {
    let total: Int
    let size: Int
    let counted: Bool
    /// The calls served, and the token whose next request fails as expired (once).
    private let served = Locked<(calls: Int, expireAt: String?)>((0, nil))

    var calls: Int { served.get().calls }
    var expireAt: String? {
        get { served.get().expireAt }
        set { served.withLock { $0.expireAt = newValue } }
    }

    init(total: Int, size: Int, counted: Bool = true) {
        self.total = total
        self.size = size
        self.counted = counted
    }

    func page(_ token: String) throws -> KubePage<Int> {
        let expired = served.withLock { s in
            s.calls += 1
            guard token == s.expireAt else { return false }
            s.expireAt = nil
            return true
        }
        if expired {
            throw PageError(message: "\(kubeListExpired): The provided continue parameter is too old")
        }
        let from = Int(token.isEmpty ? "0" : token) ?? 0
        let to = min(from + size, total)
        let complete = to == total
        return KubePage(
            items: Array(from..<to), continueToken: complete ? "" : String(to),
            remaining: complete ? 0 : (counted ? Int64(total - to) : -1), complete: complete)
    }
}

final class PagedLoadTests: XCTestCase {
    func testLoadsEveryPageReportingProgress() throws {
        let server = PageServer(total: 1_200, size: 500)
        let (load, seen) = try runBlocking { () -> (PagedLoad<Int>, [PagedLoad<Int>]) in
            var seen: [PagedLoad<Int>] = []
            let load = try await loadPages(cap: 10_000, fetch: { try server.page($0) }) { seen.append($0) }
            return (load, seen)
        }
        XCTAssertTrue(load.done)
        XCTAssertFalse(load.capped)
        XCTAssertEqual(load.items, Array(0..<1_200))
        XCTAssertEqual(seen.map(\.items.count), [500, 1_000, 1_200])
        // "500 / ~1,200" after the first page.
        XCTAssertEqual(seen.first?.estimatedTotal, 1_200)
        XCTAssertEqual(server.calls, 3)
    }

    func testOnePageListIsDoneAtOnce() throws {
        let server = PageServer(total: 42, size: 500)
        let load = try runBlocking { try await loadPages(cap: 10_000, fetch: { try server.page($0) }) }
        XCTAssertTrue(load.done)
        XCTAssertFalse(load.hasMore)
        XCTAssertEqual(load.estimatedTotal, 42)
    }

    func testStopsAtTheCapThenLoadsMoreOnScroll() throws {
        let server = PageServer(total: 2_000, size: 500)
        let load = try runBlocking { try await loadPages(cap: 1_000, fetch: { try server.page($0) }) }
        XCTAssertTrue(load.capped)
        XCTAssertTrue(load.hasMore)
        XCTAssertFalse(load.done)
        XCTAssertEqual(load.items.count, 1_000)
        XCTAssertEqual(load.estimatedTotal, 2_000)

        let more = try runBlocking { try await load.loadingMore { try server.page($0) } }
        XCTAssertEqual(more.items, Array(0..<1_500))  // server order, appended
        let last = try runBlocking { try await more.loadingMore { try server.page($0) } }
        XCTAssertTrue(last.done)
        let after = try runBlocking {
            try await last.loadingMore { _ in
                XCTFail("no page after the last"); throw PageError(message: "")
            }
        }
        XCTAssertEqual(after, last)
    }

    func testTheDefaultScopeStopsAfterItsFirstPage() throws {
        let server = PageServer(total: 20_000, size: kubePageSize)
        let load = try runBlocking {
            try await loadPages(cap: eagerLimit(scope: KubeScope(), metered: false), fetch: { try server.page($0) })
        }
        XCTAssertEqual(load.items.count, kubePageSize)
        XCTAssertTrue(load.capped)
    }

    func testAnExpiredListStartsAgainFromTheFirstPage() throws {
        let server = PageServer(total: 1_500, size: 500)
        server.expireAt = "1000"
        let (load, seen) = try runBlocking { () -> (PagedLoad<Int>, [Int]) in
            var seen: [Int] = []
            let load = try await loadPages(cap: 10_000, fetch: { try server.page($0) }) { seen.append($0.items.count) }
            return (load, seen)
        }
        XCTAssertTrue(load.done)
        XCTAssertEqual(load.items, Array(0..<1_500))  // no row twice
        XCTAssertEqual(seen, [500, 1_000, 500, 1_000, 1_500])
        XCTAssertEqual(server.calls, 6)
    }

    func testOtherErrorsAndRepeatedExpiriesPropagate() throws {
        XCTAssertThrowsError(
            try runBlocking {
                try await loadPages(
                    cap: 10_000,
                    fetch: { (_: String) -> KubePage<Int> in
                        throw PageError(message: "Kubernetes API: permission denied")
                    })
            }
        ) { error in
            XCTAssertEqual(error.localizedDescription, "Kubernetes API: permission denied")
        }

        let calls = Locked(0)
        XCTAssertThrowsError(
            try runBlocking {
                try await loadPages(
                    cap: 10_000, maxRestarts: 2,
                    fetch: { (_: String) -> KubePage<Int> in
                        calls.withLock { $0 += 1 }
                        throw PageError(message: "\(kubeListExpired): gone")
                    })
            }
        ) { error in
            XCTAssertTrue(isKubeListExpired(error))
        }
        XCTAssertEqual(calls.get(), 3)
    }

    func testUnknownRemainingCount() throws {
        let first = PagedLoad<Int>().appending(try PageServer(total: 1_000, size: 500, counted: false).page(""))
        XCTAssertNil(first.estimatedTotal)
        XCTAssertTrue(first.hasMore)
    }

    func testDetailedOnlyWhenEveryPageIs() {
        let load = PagedLoad<Int>()
            .appending(KubePage(items: [1], continueToken: "a", complete: false, detailed: true))
            .appending(KubePage(items: [2], detailed: false))
        XCTAssertFalse(load.detailed)
        XCTAssertTrue(PagedLoad.complete([1]).detailed)
        // A kept list of Table rows says so.
        XCTAssertFalse(PagedLoad.complete([1], detailed: false).detailed)
    }

    func testCapIsLowerOnMeteredNetworks() {
        XCTAssertEqual(eagerCap(metered: false), 10_000)
        XCTAssertEqual(eagerCap(metered: true), 5_000)
    }

    func testRecognisesTheExpiredMessage() {
        XCTAssertTrue(isKubeListExpired(PageError(message: "go: \(kubeListExpired): too old")))
        XCTAssertFalse(isKubeListExpired(PageError(message: "Kubernetes API: not found")))
    }

    func testListKeysPerScope() {
        XCTAssertEqual(kubeListKey("pods", namespace: nil), "pods|*")
        XCTAssertEqual(kubeListKey("workloads", namespace: "shop"), "workloads|shop")
    }
}
