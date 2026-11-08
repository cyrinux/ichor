import Foundation

/// The two file formats a backup can be saved in: the app's own (`ICHORBAK`, the default) or
/// a standard age file that `age -d` opens on a computer (Go EncryptBackupAge).
public enum BackupFileFormat: String, Sendable, CaseIterable, Identifiable {
    case ichor, age

    public var id: String { rawValue }

    /// The extension a backup of this format is saved with.
    public var fileExtension: String {
        switch self {
        case .ichor: backupExtension
        case .age: backupAgeExtension
        }
    }
}

/// The extension age backups are saved with, on both platforms.
public let backupAgeExtension = "age"

/// How an age backup is sealed: with a passphrase (an age scrypt recipient, which age allows
/// only alone) or for pasted age public keys (the phone cannot open those again).
public enum BackupAgeMode: String, Sendable, CaseIterable, Identifiable {
    case passphrase, recipients

    public var id: String { rawValue }
}

/// The lines of a pasted recipients text that hold a key: trimmed, without blank lines and
/// `#` comments (as in an age recipients file).
public func ageRecipientLines(_ text: String) -> [String] {
    text.split(whereSeparator: \.isNewline)
        .map { $0.trimmingCharacters(in: .whitespaces) }
        .filter { !isSkippedRecipientLine($0) }
}

/// Whether `line` is an age X25519 public key: "age1" then 58 bech32 characters with a valid
/// checksum (one case only). Plugin keys (age1yubikey1…, age1tag1…) and SSH keys are not.
public func isAgeRecipient(_ line: String) -> Bool {
    let lower = line.lowercased()
    guard line == lower || line == line.uppercased() else { return false }  // mixed case is invalid bech32
    guard lower.count == ageRecipientLength, lower.hasPrefix("age1") else { return false }
    var values: [UInt8] = []
    for char in lower.dropFirst(4) {
        guard let index = bech32Charset.firstIndex(of: char) else { return false }
        values.append(UInt8(bech32Charset.distance(from: bech32Charset.startIndex, to: index)))
    }
    return bech32Polymod(bech32HRPExpand("age") + values) == 1
}

/// Why a pasted recipients text cannot be used yet.
public enum AgeRecipientsProblem: Equatable, Sendable {
    case empty
    /// The 1-based number of the first line that is not an age public key (blank and comment
    /// lines counted): the line's text is never shown, a pasted secret key stays off screen.
    case invalid(line: Int)
}

public func ageRecipientsProblem(_ text: String) -> AgeRecipientsProblem? {
    let lines = text.split(omittingEmptySubsequences: false, whereSeparator: \.isNewline)
        .map { $0.trimmingCharacters(in: .whitespaces) }
    let bad = lines.indices.first { !isSkippedRecipientLine(lines[$0]) && !isAgeRecipient(lines[$0]) }
    if let bad { return .invalid(line: bad + 1) }
    if !lines.contains(where: { !isSkippedRecipientLine($0) }) { return .empty }
    return nil
}

/// The recipients as the Go core takes them: one key per line, lower case (an all upper case
/// key, as a QR code may carry it, is the same key).
public func ageRecipientsArgument(_ text: String) -> String {
    ageRecipientLines(text).map { $0.lowercased() }.joined(separator: "\n")
}

/// Whether a trimmed line holds no key: blank, or a `#` comment as in an age recipients file.
private func isSkippedRecipientLine(_ line: String) -> Bool {
    line.isEmpty || line.hasPrefix("#")
}

/// "age1" + 52 data characters (32 bytes) + 6 checksum characters.
private let ageRecipientLength = 62
private let bech32Charset = Array("qpzry9x8gf2tvdw0s3jn54khce6mua7l")

private func bech32HRPExpand(_ hrp: String) -> [UInt8] {
    let bytes = Array(hrp.utf8)
    return bytes.map { $0 >> 5 } + [0] + bytes.map { $0 & 31 }
}

private func bech32Polymod(_ values: [UInt8]) -> UInt32 {
    let generator: [UInt32] = [0x3b6a_57b2, 0x2650_8e6d, 0x1ea1_19fa, 0x3d42_33dd, 0x2a14_62b3]
    var chk: UInt32 = 1
    for value in values {
        let top = chk >> 25
        chk = (chk & 0x1ff_ffff) << 5 ^ UInt32(value)
        for i in 0..<5 where (top >> UInt32(i)) & 1 == 1 {
            chk ^= generator[i]
        }
    }
    return chk
}

/// Whether a picked backup must say at once that it opens with an age key on a computer,
/// instead of asking a passphrase: an age file (`isAge`, from Go BackupFormatOf) that the
/// empty passphrase try (Go DecryptBackup with "") failed to open with
/// `BackupError.needsIdentity`. Any other outcome (a passphrase file says wrong passphrase)
/// goes on to the passphrase.
public func pickedBackupNeedsIdentity(isAge: Bool, emptyPassphraseError: Error?) -> Bool {
    isAge && (emptyPassphraseError as? BackupError) == .needsIdentity
}
