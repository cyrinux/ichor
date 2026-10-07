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

/// The talosconfig, and the kubeconfig of the clusters added without Talos, are each encrypted
/// (ECIES, AES-GCM) to a P-256 key generated inside the Secure Enclave; the private key never
/// leaves it, so decrypting needs this device's Enclave. Only the ciphertexts are stored, in
/// the Keychain (this device only, never synced or backed up). Falls back to a Keychain key
/// where there is no Secure Enclave (e.g. the simulator).
enum SecureConfigStore {
    /// The two sealed configs, under the same key.
    enum Item: CaseIterable {
        case talosconfig, kubeconfig

        fileprivate var account: String {
            switch self {
            case .talosconfig: "talosconfig.sealed"
            case .kubeconfig: "kubeconfig.sealed"
            }
        }
    }

    private static let keyTag = Data("name.levis.ichor.config-key".utf8)
    private static let algorithm = SecKeyAlgorithm.eciesEncryptionCofactorVariableIVX963SHA256AESGCM

    static func save(_ plaintext: Data, item: Item = .talosconfig) throws {
        let key = try privateKey(create: true)
        guard let publicKey = SecKeyCopyPublicKey(key) else { throw KeychainError(status: errSecInvalidKeyRef) }
        var error: Unmanaged<CFError>?
        guard let sealed = SecKeyCreateEncryptedData(publicKey, algorithm, plaintext as CFData, &error) as Data? else {
            throw cfError(error)
        }
        try Keychain.write(sealed, account: item.account)
    }

    static func load(_ item: Item = .talosconfig) -> Data? {
        guard let sealed = Keychain.read(item.account), let key = try? privateKey(create: false) else { return nil }
        var error: Unmanaged<CFError>?
        return SecKeyCreateDecryptedData(key, algorithm, sealed as CFData, &error) as Data?
    }

    /// The stored text of `item`, nil when there is none (or it cannot be read, e.g. locked).
    static func loadText(_ item: Item) -> String? {
        load(item).flatMap { String(data: $0, encoding: .utf8) }
    }

    /// Whether a sealed `item` is stored, readable or not (the Keychain answers once unlocked).
    static func isStored(_ item: Item) -> Bool {
        Keychain.read(item.account) != nil
    }

    /// Deletes `item`; the key goes too once no config is left.
    static func delete(_ item: Item) {
        Keychain.delete(item.account)
        if Item.allCases.allSatisfy({ Keychain.read($0.account) == nil }) {
            SecItemDelete(keyQuery as CFDictionary)
        }
    }

    /// Deletes both configs and their key.
    static func delete() {
        for item in Item.allCases { Keychain.delete(item.account) }
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
