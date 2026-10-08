import CryptoKit
import Foundation
import IchorCore

/// The cryptography of the security-key modes (see IchorCore/SecurityKeys.swift): the data key
/// wrapped by a key's hmac-secret output, the stored items sealed with it, and the check of an
/// assertion. Same construction as Android's DekWrap / SealedFile / AssertionVerifier.
enum SecurityKeyCrypto {
    static let dekSize = 32
    private static let wrapInfo = Data("ichor security-key dek v1".utf8)

    static func newDek() -> Data { SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) } }

    /// Wraps `dek` with a key's hmac-secret output: HKDF-SHA256 of it gives an AES-256 key, which
    /// encrypts the DEK with AES-GCM (combined form: nonce, ciphertext, tag).
    static func wrap(dek: Data, secret: Data) throws -> Data {
        let box = try AES.GCM.seal(dek, using: wrappingKey(secret))
        guard let combined = box.combined else { throw SecurityKeyUnwrapError.damaged }
        return combined
    }

    /// Throws `SecurityKeyUnwrapError.wrongKey` when `secret` is not the one `wrapped` was made with.
    static func unwrap(wrapped: Data, secret: Data) throws -> Data {
        guard let box = try? AES.GCM.SealedBox(combined: wrapped) else { throw SecurityKeyUnwrapError.damaged }
        do {
            return try AES.GCM.open(box, using: wrappingKey(secret))
        } catch {
            throw SecurityKeyUnwrapError.wrongKey
        }
    }

    /// The DEK `assertion`'s key unwraps, after the record's checks.
    static func unwrapDek(_ enrolment: SecurityKeyEnrolment, assertion: SecurityKeyAssertion) throws -> Data {
        let (wrapped, secret) = try enrolment.wrappedDek(for: assertion)
        return try unwrap(wrapped: wrapped, secret: secret)
    }

    /// `key` with `dek` wrapped by the secret `assertion` gave (its own), for the required mode.
    static func wrapping(_ key: EnrolledKey, dek: Data, assertion: SecurityKeyAssertion) throws -> EnrolledKey {
        guard let secret = assertion.secret else { throw SecurityKeyUnwrapError.noSecret }
        var wrapped = key
        wrapped.wrappedDek = try wrap(dek: dek, secret: secret)
        wrapped.uv = assertion.uv
        return wrapped
    }

    private static func wrappingKey(_ secret: Data) -> SymmetricKey {
        HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: secret), info: wrapInfo, outputByteCount: 32)
    }

    /// `inner` (a Secure Enclave ciphertext) sealed with the DEK, in the SealedBlob layout.
    static func seal(_ inner: Data, dek: Data) throws -> Data {
        let box = try AES.GCM.seal(inner, using: SymmetricKey(data: dek))
        guard let combined = box.combined else { throw SecurityKeyUnwrapError.damaged }
        return SealedBlob.wrap(combined)
    }

    /// The inner ciphertext of a sealed `blob`; nil when `dek` is not the one it was sealed with.
    static func open(_ blob: Data, dek: Data) -> Data? {
        guard let combined = SealedBlob.unwrap(blob), let box = try? AES.GCM.SealedBox(combined: combined) else { return nil }
        return try? AES.GCM.open(box, using: SymmetricKey(data: dek))
    }

    /// Checks an assertion as a relying party would: the rpIdHash, the user-presence flag and the
    /// ES256 signature (DER, as CTAP gives it) over authenticatorData || clientDataHash with the
    /// enrolled public key. Returns whether the key verified the user, nil when the check fails.
    static func verify(_ key: EnrolledKey, authenticatorData: Data, clientDataHash: Data, signature: Data, rpId: String = securityKeyRpId) -> Bool? {
        guard let head = AuthenticatorDataHead(authenticatorData), head.userPresent else { return nil }
        guard head.rpIdHash == Data(SHA256.hash(data: Data(rpId.utf8))) else { return nil }
        guard let publicKey = try? P256.Signing.PublicKey(rawRepresentation: key.publicKey),
              let ecdsa = try? P256.Signing.ECDSASignature(derRepresentation: signature) else { return nil }
        guard publicKey.isValidSignature(ecdsa, for: authenticatorData + clientDataHash) else { return nil }
        return head.userVerified
    }
}
