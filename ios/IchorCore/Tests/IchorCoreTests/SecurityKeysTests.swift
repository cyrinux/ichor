import XCTest
@testable import IchorCore

final class SecurityKeysTests: XCTestCase {
    private func key(_ id: UInt8, wrapped: Data? = nil, uv: Bool = false) -> EnrolledKey {
        EnrolledKey(credentialId: Data([id, id]), publicKey: Data(repeating: 1, count: 64), label: "Security key",
                    enrolledAt: Date(timeIntervalSince1970: 1_700_000_000), wrappedDek: wrapped, uv: uv)
    }

    func testEnrolmentRoundTripsThroughJson() {
        let record = SecurityKeyEnrolment.create().with(key(1))
        XCTAssertEqual(record.salt.count, 32)
        XCTAssertEqual(SecurityKeyEnrolment.decode(record.encoded()), record)
        XCTAssertNil(SecurityKeyEnrolment.decode(nil))
        XCTAssertNil(SecurityKeyEnrolment.decode(Data()))
        XCTAssertNil(SecurityKeyEnrolment.decode(Data("not json".utf8)))
    }

    func testRequiredOnlyWithKeysInTheRequiredMode() {
        var record = SecurityKeyEnrolment.create()
        XCTAssertFalse(record.required)
        record.mode = .required
        XCTAssertFalse(record.required, "no key enrolled")
        record = record.with(key(1))
        XCTAssertTrue(record.required)
        XCTAssertFalse(record.released().required)
        XCTAssertFalse(record.without(credentialId: Data([1, 1])).required)
    }

    func testKeysAreReplacedByCredentialIdAndReleasedWithoutWraps() {
        let record = SecurityKeyEnrolment.create().with(key(1)).with(key(1, wrapped: Data([9]), uv: true)).with(key(2, wrapped: Data([8])))
        XCTAssertEqual(record.keys.count, 2)
        XCTAssertEqual(record.key(credentialId: Data([1, 1]))?.wrappedDek, Data([9]))
        XCTAssertTrue(record.key(credentialId: Data([1, 1]))?.uv == true)
        XCTAssertNil(record.key(credentialId: Data([3, 3])))
        let released = record.released()
        XCTAssertEqual(released.mode, .unlock)
        XCTAssertTrue(released.keys.allSatisfy { $0.wrappedDek == nil })
        XCTAssertEqual(record.without(credentialId: Data([2, 2])).keys.map(\.credentialId), [Data([1, 1])])
    }

    func testUnwrapChecks() {
        let wrapped = Data(repeating: 7, count: SealedBlob.nonceSize + 32 + SealedBlob.tagSize)
        let enrolled = key(1, wrapped: wrapped)
        let record = SecurityKeyEnrolment.create().with(enrolled)
        let secret = Data(repeating: 2, count: 32)

        let ok = try? record.wrappedDek(for: SecurityKeyAssertion(key: enrolled, secret: secret, uv: false))
        XCTAssertEqual(ok?.wrapped, wrapped)
        XCTAssertEqual(ok?.secret, secret)

        func reason(_ assertion: SecurityKeyAssertion) -> SecurityKeyUnwrapError? {
            do {
                _ = try record.wrappedDek(for: assertion)
                return nil
            } catch {
                return error as? SecurityKeyUnwrapError
            }
        }
        XCTAssertEqual(reason(SecurityKeyAssertion(key: enrolled, secret: nil, uv: false)), .noSecret)
        XCTAssertEqual(reason(SecurityKeyAssertion(key: key(1), secret: secret, uv: false)), .notWrapped)
        XCTAssertEqual(reason(SecurityKeyAssertion(key: enrolled, secret: secret, uv: true)), .uvChanged)
        XCTAssertEqual(reason(SecurityKeyAssertion(key: key(1, wrapped: Data([1, 2, 3])), secret: secret, uv: false)), .damaged)
    }

    func testAuthenticatorDataHeadParsesFlagsAndCount() {
        var data = Data(repeating: 0xAB, count: 32)
        data.append(0x05) // UP + UV
        data.append(contentsOf: [0, 0, 1, 2])
        let head = AuthenticatorDataHead(data)
        XCTAssertEqual(head?.rpIdHash, Data(repeating: 0xAB, count: 32))
        XCTAssertTrue(head?.userPresent == true)
        XCTAssertTrue(head?.userVerified == true)
        XCTAssertEqual(head?.signCount, 258)

        var upOnly = Data(repeating: 0, count: 32)
        upOnly.append(0x01)
        upOnly.append(contentsOf: [0, 0, 0, 0])
        XCTAssertTrue(AuthenticatorDataHead(upOnly)?.userPresent == true)
        XCTAssertFalse(AuthenticatorDataHead(upOnly)?.userVerified == true)
        XCTAssertNil(AuthenticatorDataHead(Data(repeating: 0, count: 36)), "too short")
    }

    func testSealedBlobLayout() {
        let combined = Data(repeating: 3, count: SealedBlob.nonceSize + 5 + SealedBlob.tagSize)
        let blob = SealedBlob.wrap(combined)
        XCTAssertTrue(SealedBlob.isSealed(blob))
        XCTAssertEqual(SealedBlob.unwrap(blob), combined)
        XCTAssertFalse(SealedBlob.isSealed(combined))
        XCTAssertNil(SealedBlob.unwrap(combined))
        XCTAssertFalse(SealedBlob.isSealed(SealedBlob.magic + Data(repeating: 0, count: SealedBlob.nonceSize + SealedBlob.tagSize)), "too short to hold a box")
    }
}
