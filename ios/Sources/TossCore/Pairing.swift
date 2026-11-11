import Foundation

/// A computer this phone is paired with. Stored in the keychain shared by the app and the
/// share extension.
public struct Pairing: Codable, Equatable, Identifiable {
    public var computerID: String
    public var computerName: String
    /// Hosts to try, in order (mDNS name first, then IP addresses).
    public var hosts: [String]
    public var port: Int
    /// base64url SHA-256 of the computer's certificate (DER).
    public var fingerprint: String
    public var deviceKey: String
    public var pairedAt: Date
    /// The host that answered last time: tried first next time.
    public var lastHost: String?
    public var lastUsed: Date?

    public var id: String { computerID }

    public init(computerID: String, computerName: String, hosts: [String], port: Int, fingerprint: String,
                deviceKey: String, pairedAt: Date = Date(), lastHost: String? = nil, lastUsed: Date? = nil) {
        self.computerID = computerID
        self.computerName = computerName
        self.hosts = hosts
        self.port = port
        self.fingerprint = fingerprint
        self.deviceKey = deviceKey
        self.pairedAt = pairedAt
        self.lastHost = lastHost
        self.lastUsed = lastUsed
    }

    /// Hosts in the order to try: the last one that worked first.
    public var hostsToTry: [String] {
        guard let lastHost else { return hosts }
        return [lastHost] + hosts.filter { $0 != lastHost }
    }
}

/// What the app and the extension keep in the shared keychain item.
public struct SharedState: Codable, Equatable {
    public var deviceID: String
    public var deviceName: String
    public var pairings: [Pairing]
    /// Convert HEIC photos to JPEG before sending (most computers can't open HEIC).
    public var convertHEIC: Bool

    public init(deviceID: String = UUID().uuidString, deviceName: String = "iPhone", pairings: [Pairing] = [], convertHEIC: Bool = true) {
        self.deviceID = deviceID
        self.deviceName = deviceName
        self.pairings = pairings
        self.convertHEIC = convertHEIC
    }

    /// Paired computers, most recently used first.
    public var pairingsByUse: [Pairing] {
        pairings.sorted { ($0.lastUsed ?? $0.pairedAt) > ($1.lastUsed ?? $1.pairedAt) }
    }

    /// Adds or replaces the pairing for the same computer.
    public mutating func upsert(_ pairing: Pairing) {
        pairings.removeAll { $0.computerID == pairing.computerID }
        pairings.append(pairing)
    }

    public mutating func update(_ id: String, _ change: (inout Pairing) -> Void) {
        if let i = pairings.firstIndex(where: { $0.computerID == id }) { change(&pairings[i]) }
    }

    public mutating func remove(_ id: String) {
        pairings.removeAll { $0.computerID == id }
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        deviceID = try c.decode(String.self, forKey: .deviceID)
        deviceName = try c.decodeIfPresent(String.self, forKey: .deviceName) ?? "iPhone"
        pairings = try c.decodeIfPresent([Pairing].self, forKey: .pairings) ?? []
        convertHEIC = try c.decodeIfPresent(Bool.self, forKey: .convertHEIC) ?? true
    }
}

/// The `toss://pair?…` link in the computer's QR code (see PROTOCOL.md).
public struct PairingLink: Equatable {
    public var computerName: String
    public var hosts: [String]
    public var port: Int
    public var fingerprint: String
    public var token: String

    public enum ParseError: Error, Equatable, CustomStringConvertible {
        case notTossLink
        case unsupportedVersion(String)
        case missing(String)

        public var description: String {
            switch self {
            case .notTossLink: return "This isn't a Toss pairing code."
            case let .unsupportedVersion(v): return "This pairing code is from a newer Toss (version \(v)). Update the app."
            case let .missing(field): return "The pairing code is incomplete (\(field) missing). Run `toss pair` again."
            }
        }
    }

    public init(computerName: String, hosts: [String], port: Int, fingerprint: String, token: String) {
        self.computerName = computerName
        self.hosts = hosts
        self.port = port
        self.fingerprint = fingerprint
        self.token = token
    }

    public init(string: String) throws {
        guard let components = URLComponents(string: string.trimmingCharacters(in: .whitespacesAndNewlines)),
              components.scheme?.lowercased() == "toss", components.host?.lowercased() == "pair" else {
            throw ParseError.notTossLink
        }
        var query: [String: String] = [:]
        for item in components.queryItems ?? [] where query[item.name] == nil {
            // Go's QueryEscape writes spaces as "+", which URLComponents leaves alone.
            query[item.name] = item.value?.replacingOccurrences(of: "+", with: " ")
        }
        guard let version = query["v"] else { throw ParseError.missing("version") }
        guard version == "1" else { throw ParseError.unsupportedVersion(version) }
        guard let hosts = query["h"]?.split(separator: ",").map({ String($0).trimmingCharacters(in: .whitespaces) }).filter({ !$0.isEmpty }),
              !hosts.isEmpty else { throw ParseError.missing("hosts") }
        guard let port = query["p"].flatMap(Int.init), (1...65535).contains(port) else { throw ParseError.missing("port") }
        guard let fp = query["fp"], Base64URL.decode(fp)?.count == 32 else { throw ParseError.missing("fingerprint") }
        guard let token = query["t"], !token.isEmpty else { throw ParseError.missing("token") }
        self.init(computerName: (query["n"] ?? "").isEmpty ? hosts[0] : query["n"]!, hosts: hosts, port: port, fingerprint: fp, token: token)
    }
}

public enum Base64URL {
    public static func encode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    public static func decode(_ string: String) -> Data? {
        var s = string.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while s.count % 4 != 0 { s += "=" }
        return Data(base64Encoded: s)
    }
}
