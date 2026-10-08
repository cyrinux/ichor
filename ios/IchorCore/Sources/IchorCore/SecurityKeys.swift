import Foundation

// Security keys (a YubiKey or any FIDO2 key, tapped over NFC or plugged in) as a way to open the
// app lock, with an optional hardened mode. Same records and rules as Android (SecurityKeys.kt).
// Only plain Foundation here, so it runs in the Linux tests: the app adds CryptoKit for the
// wrapping and the signature check (SecurityKeyCrypto.swift) and YubiKit to talk to the key.
//
// Each key holds a FIDO2 credential for the relying party `securityKeyRpId` (the app's domain,
// nothing is served there). Opening the lock asks the key for an assertion over a fresh challenge
// with user presence only (a touch, no FIDO PIN) and checks its signature with the public key
// recorded at enrolment. In the "required" mode the same assertion also carries the hmac-secret
// (WebAuthn prf) output for a fixed salt: a 32-byte secret only that key computes, stable for the
// credential. It wraps a random data-encryption key (DEK) that seals the stored configs on top of
// the Secure Enclave: without a key tapped they cannot be read.

/// The relying party id the credentials are made for.
public let securityKeyRpId = "ichor.levis.name"

/// The most keys that can be enrolled: the one worn, and a spare.
public let securityKeyMax = 2

/// What the enrolled keys are for.
public enum SecurityKeyMode: String, Codable, Sendable {
    /// An alternative to Face ID / the passcode: either one opens the lock.
    case unlock
    /// The stored configs are sealed with a key only a tap unwraps: Face ID alone no longer opens the app.
    case required
}

/// One enrolled key. Public data only (the credential id and public key; no secret).
public struct EnrolledKey: Codable, Equatable, Sendable {
    /// The credential id the key gave at enrolment.
    public var credentialId: Data
    /// Its ES256 public key, the raw 64 bytes x || y of the P-256 point.
    public var publicKey: Data
    /// How it is listed in Settings.
    public var label: String
    public var enrolledAt: Date
    /// In the required mode: the DEK wrapped with this key's hmac-secret output (see the app's
    /// SecurityKeyCrypto). Nil in the unlock mode, or before the key was tapped for it.
    public var wrappedDek: Data?
    /// Whether the output wrapping the DEK was made with user verification on the key (a key set
    /// to always verify): the authenticator derives another output then, so the same state is
    /// needed to unwrap, and the other one is refused with a clear message.
    public var uv: Bool

    public init(credentialId: Data, publicKey: Data, label: String, enrolledAt: Date = Date(), wrappedDek: Data? = nil, uv: Bool = false) {
        self.credentialId = credentialId
        self.publicKey = publicKey
        self.label = label
        self.enrolledAt = enrolledAt
        self.wrappedDek = wrappedDek
        self.uv = uv
    }
}

/// The enrolled keys and their mode; the record the app lock keeps (no secret in it).
public struct SecurityKeyEnrolment: Codable, Equatable, Sendable {
    public var keys: [EnrolledKey]
    /// The hmac-secret salt, 32 random bytes made at the first enrolment.
    public var salt: Data
    public var mode: SecurityKeyMode

    public init(keys: [EnrolledKey] = [], salt: Data, mode: SecurityKeyMode = .unlock) {
        self.keys = keys
        self.salt = salt
        self.mode = mode
    }

    /// A new record, with no key yet, for the first enrolment.
    public static func create() -> SecurityKeyEnrolment {
        SecurityKeyEnrolment(salt: Data((0..<32).map { _ in UInt8.random(in: .min ... .max) }))
    }

    /// Whether a tap is needed to read the stored configs (and Face ID alone is refused).
    public var required: Bool { mode == .required && !keys.isEmpty }

    public func key(credentialId: Data) -> EnrolledKey? { keys.first { $0.credentialId == credentialId } }

    /// With `key` added, or replacing the enrolled key of the same credential.
    public func with(_ key: EnrolledKey) -> SecurityKeyEnrolment {
        var copy = self
        copy.keys = keys.filter { $0.credentialId != key.credentialId } + [key]
        return copy
    }

    public func without(credentialId: Data) -> SecurityKeyEnrolment {
        var copy = self
        copy.keys = keys.filter { $0.credentialId != credentialId }
        return copy
    }

    /// The same keys in the unlock mode, their wrapped DEKs dropped.
    public func released() -> SecurityKeyEnrolment {
        var copy = self
        copy.mode = .unlock
        copy.keys = keys.map { key in
            var released = key
            released.wrappedDek = nil
            return released
        }
        return copy
    }

    public func encoded() -> Data {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .millisecondsSince1970
        return (try? encoder.encode(self)) ?? Data()
    }

    /// The stored record, nil for none or one that cannot be read.
    public static func decode(_ data: Data?) -> SecurityKeyEnrolment? {
        guard let data, !data.isEmpty else { return nil }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .millisecondsSince1970
        return try? decoder.decode(SecurityKeyEnrolment.self, from: data)
    }
}

/// What a tap of an enrolled key gave: which key, its hmac-secret output when asked for, and
/// whether the key verified the user.
public struct SecurityKeyAssertion: Sendable {
    public let key: EnrolledKey
    public let secret: Data?
    public let uv: Bool

    public init(key: EnrolledKey, secret: Data?, uv: Bool) {
        self.key = key
        self.secret = secret
        self.uv = uv
    }
}

/// Why the DEK could not be unwrapped with what the key gave; the app shows a message per case.
public enum SecurityKeyUnwrapError: Error, Equatable, Sendable {
    /// The key returned no hmac-secret output (no support for the extension).
    case noSecret
    /// This key was enrolled without a wrapped DEK (before the requirement was turned on).
    case notWrapped
    /// The key verified the user this time but not at enrolment (or the reverse): other output.
    case uvChanged
    /// The output does not unwrap the DEK: not the secret the wrap was made with.
    case wrongKey
    /// The wrapped DEK is too short to be one.
    case damaged
}

extension SecurityKeyEnrolment {
    /// The wrapped DEK and the secret to unwrap it with, from `assertion`; the checks before the
    /// (CryptoKit) decryption, which the app does.
    public func wrappedDek(for assertion: SecurityKeyAssertion) throws -> (wrapped: Data, secret: Data) {
        guard let secret = assertion.secret else { throw SecurityKeyUnwrapError.noSecret }
        guard let wrapped = assertion.key.wrappedDek else { throw SecurityKeyUnwrapError.notWrapped }
        guard assertion.uv == assertion.key.uv else { throw SecurityKeyUnwrapError.uvChanged }
        guard wrapped.count > SealedBlob.nonceSize + SealedBlob.tagSize else { throw SecurityKeyUnwrapError.damaged }
        return (wrapped, secret)
    }
}

/// The fixed-size head of a WebAuthn authenticatorData, as a relying party reads it.
public struct AuthenticatorDataHead: Equatable, Sendable {
    public let rpIdHash: Data
    public let flags: UInt8
    public let signCount: UInt32

    public var userPresent: Bool { flags & 0x01 != 0 }
    public var userVerified: Bool { flags & 0x04 != 0 }

    /// Nil when `data` is shorter than the 37 bytes every authenticatorData starts with.
    public init?(_ data: Data) {
        guard data.count >= 37 else { return nil }
        let bytes = [UInt8](data)
        rpIdHash = Data(bytes[0..<32])
        flags = bytes[32]
        signCount = UInt32(bytes[33]) << 24 | UInt32(bytes[34]) << 16 | UInt32(bytes[35]) << 8 | UInt32(bytes[36])
    }
}

/// The layout of a stored item sealed with the DEK in the required mode: `magic` then the
/// AES-GCM combined form (12-byte nonce, ciphertext, 16-byte tag). An item without the magic is
/// Secure Enclave-only.
public enum SealedBlob {
    /// "IK" + version 2.
    public static let magic = Data([0x49, 0x4B, 0x02])
    public static let nonceSize = 12
    public static let tagSize = 16

    public static func isSealed(_ blob: Data) -> Bool {
        blob.count > magic.count + nonceSize + tagSize && blob.prefix(magic.count) == magic
    }

    /// The sealed form of `combined` (an AES-GCM combined box).
    public static func wrap(_ combined: Data) -> Data { magic + combined }

    /// The AES-GCM combined box of a sealed `blob`, nil when it is not one.
    public static func unwrap(_ blob: Data) -> Data? {
        guard isSealed(blob) else { return nil }
        return blob.dropFirst(magic.count)
    }
}
