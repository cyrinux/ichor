import Foundation
import Security

enum ConfigProtection: String {
    case secureEnclave = "Secure Enclave"
    case keychain = "Keychain (no Secure Enclave on this device)"

    var label: String {
        switch self {
        case .secureEnclave: String(localized: "Secure Enclave")
        case .keychain: String(localized: "Keychain (no Secure Enclave on this device)")
        }
    }
}

/// The talosconfig is encrypted (ECIES, AES-GCM) to a P-256 key generated inside the Secure
/// Enclave; the private key never leaves it, so decrypting needs this device's Enclave. Only
/// the ciphertext is stored, in the Keychain (this device only, never synced or backed up).
/// Falls back to a Keychain key where there is no Secure Enclave (e.g. the simulator).
enum SecureConfigStore {
    private static let keyTag = Data("name.levis.ichor.config-key".utf8)
    private static let account = "talosconfig.sealed"
    private static let algorithm = SecKeyAlgorithm.eciesEncryptionCofactorVariableIVX963SHA256AESGCM

    static func save(_ plaintext: Data) throws {
        let key = try privateKey(create: true)
        guard let publicKey = SecKeyCopyPublicKey(key) else { throw KeychainError(status: errSecInvalidKeyRef) }
        var error: Unmanaged<CFError>?
        guard let sealed = SecKeyCreateEncryptedData(publicKey, algorithm, plaintext as CFData, &error) as Data? else {
            throw cfError(error)
        }
        try Keychain.write(sealed, account: account)
    }

    static func load() -> Data? {
        guard let sealed = Keychain.read(account), let key = try? privateKey(create: false) else { return nil }
        var error: Unmanaged<CFError>?
        return SecKeyCreateDecryptedData(key, algorithm, sealed as CFData, &error) as Data?
    }

    static func delete() {
        Keychain.delete(account)
        SecItemDelete(keyQuery as CFDictionary)
    }

    /// Where the key protecting the stored config lives (nil before the first import).
    static var protection: ConfigProtection? {
        guard let key = try? privateKey(create: false),
              let attributes = SecKeyCopyAttributes(key) as? [String: Any] else { return nil }
        let token = attributes[kSecAttrTokenID as String] as? String
        return token == (kSecAttrTokenIDSecureEnclave as String) ? .secureEnclave : .keychain
    }

    private static var keyQuery: [String: Any] {
        [
            kSecClass as String: kSecClassKey,
            kSecAttrApplicationTag as String: keyTag,
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
        ]
    }

    private static func privateKey(create: Bool) throws -> SecKey {
        var query = keyQuery
        query[kSecReturnRef as String] = true
        var item: CFTypeRef?
        if SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let item {
            return item as! SecKey // swiftlint:disable:this force_cast (CF type, always SecKey here)
        }
        guard create else { throw KeychainError(status: errSecItemNotFound) }
        if let key = try? makeKey(secureEnclave: true) { return key }
        return try makeKey(secureEnclave: false)
    }

    private static func makeKey(secureEnclave: Bool) throws -> SecKey {
        var error: Unmanaged<CFError>?
        guard let access = SecAccessControlCreateWithFlags(
            nil,
            kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
            secureEnclave ? .privateKeyUsage : [],
            &error
        ) else { throw cfError(error) }

        var attributes: [String: Any] = [
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecAttrKeySizeInBits as String: 256,
            kSecPrivateKeyAttrs as String: [
                kSecAttrIsPermanent as String: true,
                kSecAttrApplicationTag as String: keyTag,
                kSecAttrAccessControl as String: access,
            ] as [String: Any],
        ]
        if secureEnclave {
            attributes[kSecAttrTokenID as String] = kSecAttrTokenIDSecureEnclave
        }
        guard let key = SecKeyCreateRandomKey(attributes as CFDictionary, &error) else { throw cfError(error) }
        return key
    }

    private static func cfError(_ error: Unmanaged<CFError>?) -> Error {
        error?.takeRetainedValue() as Error? ?? KeychainError(status: errSecParam)
    }
}
