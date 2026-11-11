import Foundation
import Security
import TossCore

/// Keeps `SharedState` in one keychain item in an access group shared by the app and the
/// share extension (`$(AppIdentifierPrefix)com.honza.Toss.shared` in both entitlements).
public enum KeychainStore {
    static let groupSuffix = "com.honza.Toss.shared"
    static let service = "com.honza.Toss"
    static let account = "state"

    /// "TEAMID.com.honza.Toss.shared". The team prefix isn't known at compile time, so it's
    /// read from the access group the keychain assigns to an item by default.
    static let accessGroup: String? = {
        let probe: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: "com.honza.Toss.probe",
            kSecAttrAccount as String: "probe",
            kSecReturnAttributes as String: true,
        ]
        var result: CFTypeRef?
        var status = SecItemCopyMatching(probe as CFDictionary, &result)
        if status == errSecItemNotFound {
            var add = probe
            add[kSecValueData as String] = Data()
            add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            status = SecItemAdd(add as CFDictionary, &result)
        }
        guard status == errSecSuccess, let attributes = result as? [String: Any],
              let group = attributes[kSecAttrAccessGroup as String] as? String,
              let prefix = group.split(separator: ".").first else { return nil }
        return "\(prefix).\(groupSuffix)"
    }()

    private static func query() -> [String: Any] {
        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let accessGroup { q[kSecAttrAccessGroup as String] = accessGroup }
        return q
    }

    public enum StoreError: LocalizedError {
        case keychain(OSStatus)
        public var errorDescription: String? {
            switch self {
            case let .keychain(status):
                let message = SecCopyErrorMessageString(status, nil) as String? ?? "error \(status)"
                return "Couldn't use the keychain: \(message)"
            }
        }
    }

    /// The stored state, or a fresh one (new device ID) the first time.
    public static func load() -> SharedState {
        var q = query()
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        if SecItemCopyMatching(q as CFDictionary, &result) == errSecSuccess, let data = result as? Data,
           let state = try? JSONDecoder.toss.decode(SharedState.self, from: data) {
            return state
        }
        return SharedState()
    }

    public static func save(_ state: SharedState) throws {
        let data = try JSONEncoder.toss.encode(state)
        let update: [String: Any] = [kSecValueData as String: data]
        var status = SecItemUpdate(query() as CFDictionary, update as CFDictionary)
        if status == errSecItemNotFound {
            var add = query()
            add[kSecValueData as String] = data
            // The share extension runs while the phone is unlocked; AfterFirstUnlock also lets
            // a share finish if the phone locks mid-upload.
            add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            status = SecItemAdd(add as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw StoreError.keychain(status) }
    }

    /// Loads, changes and saves the state in one go.
    @discardableResult
    public static func modify(_ change: (inout SharedState) -> Void) throws -> SharedState {
        var state = load()
        change(&state)
        try save(state)
        return state
    }

    /// For the app's diagnostics: whether the shared group is in use.
    public static var sharedGroupName: String? { accessGroup }
}

extension JSONEncoder {
    static let toss: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()
}

extension JSONDecoder {
    static let toss: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()
}
