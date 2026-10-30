import Foundation
import IchorCore

/// The enrolled security keys, as stored (UserDefaults: public data only, see
/// IchorCore/SecurityKeys.swift). AppModel mirrors it for the views; Authenticator and the
/// stores read it directly.
enum SecurityKeyStore {
    private static let key = "securityKeys"

    static var current: SecurityKeyEnrolment? {
        get { SecurityKeyEnrolment.decode(UserDefaults.standard.data(forKey: key)) }
        set {
            if let newValue, !newValue.keys.isEmpty {
                UserDefaults.standard.set(newValue.encoded(), forKey: key)
            } else {
                UserDefaults.standard.removeObject(forKey: key)
            }
        }
    }

    /// Whether a tap is needed to read the stored configs (the items are sealed with the DEK).
    static var required: Bool { current?.required == true }
}

/// The data key a tapped key unwrapped (or a new one, when the requirement is turned on), for the
/// life of the process: the stored items are sealed with it (SecureConfigStore), so a cold start
/// reads nothing until a key is tapped, while the app, once open, keeps working through relocks.
/// Read from Go's threads too (KubeAuthStore), hence the lock.
final class SecurityKeySession: Sendable {
    static let shared = SecurityKeySession()

    private let dek = Locked<Data?>(nil)

    var currentDek: Data? {
        dek.get()
    }

    func provide(_ dek: Data) {
        self.dek.set(dek)
    }

    func clear() {
        dek.set(nil)
    }
}
