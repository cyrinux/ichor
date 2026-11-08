import XCTest
@testable import IchorCore

final class BackupAgeTests: XCTestCase {
    // The example key of the age README (not anyone's real key).
    private let key = "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"

    func testAcceptsAnAgeKey() {
        XCTAssertTrue(isAgeRecipient(key))
        XCTAssertTrue(isAgeRecipient(key.uppercased()))
    }

    func testRefusesOtherKeys() {
        XCTAssertFalse(isAgeRecipient(""))
        XCTAssertFalse(isAgeRecipient("age1"))
        XCTAssertFalse(isAgeRecipient(String(key.dropLast())))  // too short
        XCTAssertFalse(isAgeRecipient(key + "q"))  // too long
        XCTAssertFalse(isAgeRecipient("AGE1" + key.dropFirst(4)))  // mixed case
        XCTAssertFalse(isAgeRecipient("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample admin@laptop"))
        XCTAssertFalse(isAgeRecipient("age1tag1" + String(repeating: "q", count: 54)))  // plugin key: "1" is not bech32
        XCTAssertFalse(isAgeRecipient("AGE-SECRET-KEY-1" + String(repeating: "Q", count: 46)))
    }

    func testRefusesABadChecksum() {
        // Last character changed: same length and charset, wrong checksum.
        let flipped = String(key.dropLast()) + (key.hasSuffix("p") ? "q" : "p")
        XCTAssertFalse(isAgeRecipient(flipped))
        // A character outside the bech32 charset ("b").
        XCTAssertFalse(isAgeRecipient("age1b" + key.dropFirst(5)))
    }

    func testRecipientLines() {
        let text = "  \(key)  \n\n# laptop\r\n\(key)\n   \n"
        XCTAssertEqual(ageRecipientLines(text), [key, key])
        XCTAssertEqual(ageRecipientsArgument(text), "\(key)\n\(key)")
        XCTAssertEqual(ageRecipientLines(" \n# only a comment\n"), [])
    }

    func testRecipientsProblem() {
        XCTAssertEqual(ageRecipientsProblem(""), .empty)
        XCTAssertEqual(ageRecipientsProblem("# nothing\n"), .empty)
        XCTAssertNil(ageRecipientsProblem("\(key)\n"))
        XCTAssertEqual(ageRecipientsProblem("\(key)\nage1nope\n"), .invalid(line: 2))
        // Blank and comment lines count, as in the editor (and Android).
        XCTAssertEqual(ageRecipientsProblem("# laptop\n\n\(key)\r\nAGE-SECRET-KEY-1QQ\n"), .invalid(line: 4))
        XCTAssertEqual(ageRecipientsProblem("nope"), .invalid(line: 1))
        XCTAssertNil(ageRecipientsProblem(key.uppercased()))
    }

    func testRecipientsArgumentLowercasesKeys() {
        XCTAssertEqual(ageRecipientsArgument(" \(key.uppercased()) \n# c\n\(key)"), "\(key)\n\(key)")
    }

    func testFileNames() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let date = Date(timeIntervalSince1970: 1_790_899_200)  // 2026-10-02
        XCTAssertEqual(backupFileName(date: date, format: .age, calendar: calendar), "ichor-2026-10-02.age")
        XCTAssertEqual(backupFileName(date: date, format: .ichor, calendar: calendar), "ichor-2026-10-02.ichorbackup")
    }

    func testNeedsIdentityCode() {
        XCTAssertEqual(
            BackupError(coreMessage: "backup-needs-identity: this backup opens with your age key on a computer"),
            .needsIdentity)
        XCTAssertEqual(BackupError(coreMessage: "backup-needs-identity"), .needsIdentity)
    }

    func testKeysOnlyAgeFileNeedsIdentity() {
        XCTAssertTrue(pickedBackupNeedsIdentity(isAge: true, emptyPassphraseError: BackupError.needsIdentity))
    }

    func testOtherFilesAskThePassphrase() {
        // A passphrase age file: the empty try is a wrong passphrase.
        XCTAssertFalse(pickedBackupNeedsIdentity(isAge: true, emptyPassphraseError: BackupError.wrongPassphrase))
        XCTAssertFalse(pickedBackupNeedsIdentity(isAge: true, emptyPassphraseError: CancellationError()))
        XCTAssertFalse(pickedBackupNeedsIdentity(isAge: true, emptyPassphraseError: nil))
        XCTAssertFalse(pickedBackupNeedsIdentity(isAge: false, emptyPassphraseError: BackupError.needsIdentity))
    }
}
