import CoreNFC
import Foundation
import IchorCore
import YubiKit

/// How the key is reached: tapped on the iPhone, or plugged in (USB-C, or Lightning for a 5Ci).
enum SecurityKeyTransport {
    case nfc, wired
}

/// What went wrong with a security key, beyond what the SDK reports.
enum SecurityKeyError: LocalizedError {
    /// The assertion came from a credential that is not enrolled (another key, or a reset one).
    case notEnrolled
    /// The key answered, but its signature did not check out.
    case badSignature
    /// The credential's public key is not an ES256 one.
    case unsupportedKey
    /// The key only makes new credentials with its FIDO PIN (asked once, at enrolment).
    case pinRequired
    case alreadyEnrolled
    case noNFC

    var errorDescription: String? {
        switch self {
        case .notEnrolled: String(localized: "This security key is not enrolled.")
        case .badSignature: String(localized: "This security key cannot unlock the stored config.")
        case .unsupportedKey: String(localized: "This key made a credential Ichor cannot verify.")
        case .pinRequired: String(localized: "This key asks for its FIDO PIN, which Ichor does not use to unlock.")
        case .alreadyEnrolled: String(localized: "This key is already enrolled.")
        case .noNFC: String(localized: "This device cannot read NFC. Plug the key in instead.")
        }
    }
}

/// Set when the SDK asked for the key's PIN during a ceremony run without one.
private final class PinAsked: @unchecked Sendable {
    private let lock = NSLock()
    private var flag = false

    func set() {
        lock.lock()
        flag = true
        lock.unlock()
    }

    var value: Bool {
        lock.lock()
        defer { lock.unlock() }
        return flag
    }
}

/// Talks to a security key with Yubico's SDK: opens it over NFC (the system sheet) or a wired
/// port, then makes or asserts the app's FIDO2 credential on it. Everything about what is stored
/// and checked is in IchorCore/SecurityKeys.swift and SecurityKeyCrypto; this is the transport.
enum SecurityKeyClient {
    private static var origin: WebAuthn.Origin {
        // A literal https origin: cannot fail.
        guard let origin = try? WebAuthn.Origin("https://\(securityKeyRpId)") else { preconditionFailure("invalid relying party origin") }
        return origin
    }

    static var nfcAvailable: Bool { NFCTagReaderSession.readingAvailable }

    /// Runs `body` with the key reached over `transport`, then closes the connection. Over NFC the
    /// system sheet shows `message` and the key must stay on the phone until `body` returns.
    static func withConnection<T: Sendable>(_ transport: SecurityKeyTransport, message: String,
                                            body: (SmartCardConnection) async throws -> T) async throws -> T {
        let connection: SmartCardConnection
        switch transport {
        case .nfc:
            guard nfcAvailable else { throw SecurityKeyError.noNFC }
            connection = try await NFCSmartCardConnection(alertMessage: message)
        case .wired:
            connection = try await WiredSmartCardConnection.makeConnection()
        }
        do {
            let result = try await body(connection)
            await connection.close(error: nil)
            return result
        } catch {
            await connection.close(error: error)
            throw error
        }
    }

    private static func makeClient(_ connection: SmartCardConnection) async throws -> WebAuthn.Client {
        let session = try await CTAP2.Session.makeSession(connection: connection)
        return WebAuthn.Client(session: session, origin: origin, isPublicSuffix: { _ in false })
    }

    /// Makes the app's credential on the key: a non-resident ES256 key for `securityKeyRpId` with
    /// the hmac-secret (prf) extension enabled, user verification discouraged. The keys already in
    /// `enrolment` are excluded, so the same key cannot be added twice. A key that only makes
    /// credentials with its FIDO PIN throws `SecurityKeyError.pinRequired`: call again with `pin`.
    static func enrol(_ connection: SmartCardConnection, enrolment: SecurityKeyEnrolment, label: String, pin: String? = nil) async throws -> EnrolledKey {
        let client = try await makeClient(connection)
        let options = WebAuthn.Registration.Options(
            challenge: random(32),
            rp: WebAuthn.RelyingParty(id: securityKeyRpId, name: "Ichor"),
            user: WebAuthn.User(id: Data("ichor".utf8), name: "ichor", displayName: "Ichor"),
            excludeCredentials: enrolment.keys.map { WebAuthn.CredentialDescriptor(id: $0.credentialId) },
            residentKey: .discouraged,
            userVerification: .discouraged,
            attestation: .none,
            pubKeyCredParams: [.es256],
            extensions: WebAuthn.Extension.RegistrationInputs(prf: .enable)
        )
        let pinAsked = PinAsked()
        let authorization = pin.map { WebAuthn.Authorization.pin($0) }
            ?? WebAuthn.Authorization(providePIN: { pinAsked.set(); return .cancel }, uv: .skipped)
        let response: WebAuthn.Registration.Response
        do {
            response = try await client.makeCredential(options, authorization: authorization).value
        } catch {
            if pinAsked.value { throw SecurityKeyError.pinRequired }
            if let clientError = error as? WebAuthn.ClientError, case .credentialExcluded = clientError { throw SecurityKeyError.alreadyEnrolled }
            throw error
        }
        guard case .ec2(_, _, _, let x, let y) = response.publicKey, x.count == 32, y.count == 32 else { throw SecurityKeyError.unsupportedKey }
        return EnrolledKey(credentialId: response.credentialId, publicKey: x + y, label: label)
    }

    /// Asks the key for an assertion over a fresh challenge with one of `enrolment`'s credentials
    /// (user presence only: a touch, no PIN) and checks it. With `wantSecret`, also the hmac-secret
    /// output for the enrolment's salt (the prf extension), which the required mode unwraps the
    /// data key with.
    static func assert(_ connection: SmartCardConnection, enrolment: SecurityKeyEnrolment, wantSecret: Bool) async throws -> SecurityKeyAssertion {
        let client = try await makeClient(connection)
        let clientDataHash = random(32)
        let options = WebAuthn.Authentication.Options(
            challenge: clientDataHash,
            rpId: securityKeyRpId,
            allowCredentials: enrolment.keys.map { WebAuthn.CredentialDescriptor(id: $0.credentialId) },
            userVerification: .discouraged,
            extensions: wantSecret ? WebAuthn.Extension.AuthenticationInputs(prf: .eval(first: enrolment.salt)) : nil
        )
        // The hash is signed as given, so it is what the signature is checked over.
        let clientData = WebAuthn.ClientData.hash(clientDataHash, origin: origin, rpId: securityKeyRpId)
        let authorization = WebAuthn.Authorization(providePIN: { .cancel }, uv: .skipped)
        let responses: [WebAuthn.Authentication.Response]
        do {
            responses = try await client.getAssertion(options, clientData: clientData, authorization: authorization).value
        } catch {
            if let clientError = error as? WebAuthn.ClientError, case .noCredentials = clientError { throw SecurityKeyError.notEnrolled }
            throw error
        }
        guard let response = responses.first, let key = enrolment.key(credentialId: response.credentialId) else { throw SecurityKeyError.notEnrolled }
        guard let uv = SecurityKeyCrypto.verify(key, authenticatorData: response.rawAuthenticatorData, clientDataHash: clientDataHash, signature: response.signature) else {
            throw SecurityKeyError.badSignature
        }
        return SecurityKeyAssertion(key: key, secret: wantSecret ? response.clientExtensionResults.prf?.results.first : nil, uv: uv)
    }

    private static func random(_ count: Int) -> Data {
        Data((0..<count).map { _ in UInt8.random(in: .min ... .max) })
    }
}

/// What went wrong with a security key, as shown to the user.
func securityKeyMessage(_ error: Error) -> String {
    switch error {
    case let error as SecurityKeyError:
        return error.errorDescription ?? String(describing: error)
    case let error as SecurityKeyUnwrapError:
        switch error {
        case .noSecret: return String(localized: "This key cannot seal the configs: it lacks the hmac-secret extension.")
        case .notWrapped: return String(localized: "This key was added before the key requirement. Tap another enrolled key.")
        case .uvChanged: return String(localized: "This key's PIN or fingerprint setting changed since it was set up, so it computes another secret. Turn the key requirement off with another key, then on again.")
        case .wrongKey, .damaged: return String(localized: "This security key cannot unlock the stored config.")
        }
    case let error as WebAuthn.ClientError:
        switch error {
        case .cancelled: return String(localized: "Cancelled")
        case .timeout: return String(localized: "The key was not touched in time.")
        case .noCredentials: return String(localized: "This security key is not enrolled.")
        case .credentialExcluded: return String(localized: "This key is already enrolled.")
        case .pinRejected(let retries, _): return String(localized: "Wrong PIN, \(retries) tries left.")
        case .pinBlocked, .pinAuthBlocked: return String(localized: "The key's PIN is blocked.")
        default: return String(localized: "Could not talk to the security key (\(String(describing: error))). Tap it again.")
        }
    default:
        return String(localized: "Could not talk to the security key (\(error.localizedDescription)). Tap it again.")
    }
}
