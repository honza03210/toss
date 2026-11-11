import CryptoKit
import Foundation
import TossCore

/// Talks to a computer running `toss`: HTTPS, trusting only the certificate whose fingerprint
/// came from the pairing QR code.
public final class TossClient: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    public enum ClientError: LocalizedError {
        case unreachable(String, [String])
        case server(String)
        case badResponse

        public var errorDescription: String? {
            switch self {
            case let .unreachable(name, hosts):
                return "Can't reach \(name) (tried \(hosts.joined(separator: ", "))). Is Toss running there (toss status), and is this iPhone on the same network?"
            case let .server(message): return message
            case .badResponse: return "The computer sent an unexpected answer."
            }
        }
    }

    private let fingerprint: String
    private var progressHandler: (@Sendable (Double) -> Void)?
    private lazy var session: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 30
        config.timeoutIntervalForResource = 60 * 60
        config.waitsForConnectivity = false
        return URLSession(configuration: config, delegate: self, delegateQueue: nil)
    }()

    public init(fingerprint: String) {
        self.fingerprint = fingerprint
    }

    deinit { session.invalidateAndCancel() }

    // MARK: Certificate pinning

    public func urlSession(_ session: URLSession, didReceive challenge: URLAuthenticationChallenge,
                           completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              let trust = challenge.protectionSpace.serverTrust,
              let chain = SecTrustCopyCertificateChain(trust) as? [SecCertificate], let leaf = chain.first else {
            return completionHandler(.performDefaultHandling, nil)
        }
        let der = SecCertificateCopyData(leaf) as Data
        let actual = Base64URL.encode(Data(SHA256.hash(data: der)))
        if actual == fingerprint {
            completionHandler(.useCredential, URLCredential(trust: trust))
        } else {
            completionHandler(.cancelAuthenticationChallenge, nil)
        }
    }

    public func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64,
                           totalBytesSent: Int64, totalBytesExpectedToSend: Int64) {
        guard totalBytesExpectedToSend > 0 else { return }
        progressHandler?(Double(totalBytesSent) / Double(totalBytesExpectedToSend))
    }

    // MARK: Requests

    static func url(host: String, port: Int, path: String) -> URL {
        // IPv6 literals need brackets; names and IPv4 don't.
        let h = host.contains(":") ? "[\(host)]" : host
        return URL(string: "https://\(h):\(port)\(path)")!
    }

    private func check(_ data: Data, _ response: URLResponse) throws -> Data {
        guard let http = response as? HTTPURLResponse else { throw ClientError.badResponse }
        guard (200..<300).contains(http.statusCode) else { throw ClientError.server(serverMessage(status: http.statusCode, body: data)) }
        return data
    }

    /// Tries all hosts at once and returns the first that answers `ping`.
    public func reachableHost(name: String, hosts: [String], port: Int, deviceKey: String, timeout: TimeInterval = 3) async throws -> String {
        try await withThrowingTaskGroup(of: String?.self) { group in
            for (i, host) in hosts.enumerated() {
                group.addTask {
                    // A small head start for earlier hosts, so the preferred one wins ties.
                    try? await Task.sleep(nanoseconds: UInt64(i) * 150_000_000)
                    var request = URLRequest(url: Self.url(host: host, port: port, path: "/v1/ping"), timeoutInterval: timeout)
                    request.setValue("Bearer \(deviceKey)", forHTTPHeaderField: "Authorization")
                    guard let (data, response) = try? await self.session.data(for: request) else { return nil }
                    if let http = response as? HTTPURLResponse, http.statusCode == 401 {
                        throw ClientError.server(serverMessage(status: 401, body: data))
                    }
                    return (response as? HTTPURLResponse)?.statusCode == 200 ? host : nil
                }
            }
            for try await result in group {
                if let host = result {
                    group.cancelAll()
                    return host
                }
            }
            throw ClientError.unreachable(name, hosts)
        }
    }

    /// Pairs with the computer from a scanned link. Retries for a while, because the first
    /// connection waits on the Local Network permission prompt.
    public static func pair(link: PairingLink, deviceID: String, deviceName: String) async throws -> Pairing {
        let client = TossClient(fingerprint: link.fingerprint)
        let body = try JSONSerialization.data(withJSONObject: ["token": link.token, "deviceName": deviceName, "deviceId": deviceID])
        let deadline = Date().addingTimeInterval(25)
        var lastError: Error = ClientError.unreachable(link.computerName, link.hosts)
        while Date() < deadline {
            for host in link.hosts {
                var request = URLRequest(url: url(host: host, port: link.port, path: "/v1/pair"), timeoutInterval: 4)
                request.httpMethod = "POST"
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.httpBody = body
                do {
                    let (data, response) = try await client.session.data(for: request)
                    let checked = try client.check(data, response)
                    guard let json = try JSONSerialization.jsonObject(with: checked) as? [String: String],
                          let key = json["deviceKey"], let id = json["computerId"] else { throw ClientError.badResponse }
                    return Pairing(computerID: id, computerName: json["computerName"] ?? link.computerName, hosts: link.hosts,
                                   port: link.port, fingerprint: link.fingerprint, deviceKey: key, lastHost: host)
                } catch let error as ClientError {
                    if case .server = error { throw error } // the computer said no: don't retry
                    lastError = error
                } catch {
                    lastError = error
                }
            }
            try await Task.sleep(nanoseconds: 1_500_000_000)
        }
        if lastError is URLError { throw ClientError.unreachable(link.computerName, link.hosts) }
        throw lastError
    }

    /// The computer's clipboard text.
    public func clipboard(host: String, pairing: Pairing) async throws -> String {
        var request = URLRequest(url: Self.url(host: host, port: pairing.port, path: "/v1/clipboard"), timeoutInterval: 10)
        request.setValue("Bearer \(pairing.deviceKey)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await session.data(for: request)
        let checked = try check(data, response)
        guard let json = try JSONSerialization.jsonObject(with: checked) as? [String: String], let text = json["text"] else {
            throw ClientError.badResponse
        }
        return text
    }

    /// Sends items as one batch. `progress` gets the overall fraction (by bytes).
    public func send(_ items: [OutgoingItem], host: String, pairing: Pairing,
                     progress: @escaping @Sendable (Double) -> Void) async throws {
        let batch = UUID().uuidString
        let sizes = items.map { item -> Int64 in
            switch item {
            case let .file(url, _, _):
                return Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 1)
            default:
                return Int64(ItemRequest.body(for: item)?.count ?? 1)
            }
        }
        let total = Double(max(1, sizes.reduce(0, +)))
        var done: Int64 = 0
        for (index, item) in items.enumerated() {
            var request = URLRequest(url: Self.url(host: host, port: pairing.port, path: "/v1/items"))
            request.httpMethod = "POST"
            for (k, v) in ItemRequest.headers(for: item, deviceKey: pairing.deviceKey, batch: batch, index: index, count: items.count) {
                request.setValue(v, forHTTPHeaderField: k)
            }
            let base = Double(done)
            let size = sizes[index]
            if case let .file(url, _, mime) = item {
                if let date = await MediaDate.captureDate(of: url, mime: mime) {
                    request.setValue(formatRFC3339(date), forHTTPHeaderField: "X-Toss-Modified")
                }
                try await uploadResumable(url, size: size, request: request, host: host, pairing: pairing) { sent in
                    progress((base + Double(sent)) / total)
                }
            } else {
                progressHandler = { fraction in progress((base + fraction * Double(size)) / total) }
                let (data, response) = try await session.upload(for: request, from: ItemRequest.body(for: item) ?? Data())
                _ = try check(data, response)
            }
            done += size
            progress(Double(done) / total)
        }
        progressHandler = nil
    }

    /// Uploads a file so that a dropped connection (Wi-Fi blip, walking out of range for a moment)
    /// continues where it stopped instead of starting over: the computer keeps what arrived,
    /// says how much that is, and gets the rest (PROTOCOL.md, resumable uploads).
    func uploadResumable(_ file: URL, size: Int64, request: URLRequest, host: String, pairing: Pairing,
                         progress: @escaping @Sendable (Int64) -> Void) async throws {
        let uploadID = UUID().uuidString
        var request = request
        request.setValue(uploadID, forHTTPHeaderField: "X-Toss-Upload")
        request.setValue(String(size), forHTTPHeaderField: "X-Toss-Size")
        var offset: Int64 = 0
        var attempt = 0
        while true {
            var r = request
            r.setValue(String(offset), forHTTPHeaderField: "X-Toss-Offset")
            let start = offset
            progressHandler = { fraction in progress(start + Int64(fraction * Double(size - start))) }
            do {
                let (data, response): (Data, URLResponse)
                if offset == 0 {
                    (data, response) = try await session.upload(for: r, fromFile: file)
                } else {
                    guard let stream = InputStream(url: file) else { throw CocoaError(.fileReadUnknown) }
                    stream.setProperty(NSNumber(value: offset), forKey: .fileCurrentOffsetKey)
                    r.httpBodyStream = stream
                    r.setValue(String(size - offset), forHTTPHeaderField: "Content-Length")
                    (data, response) = try await session.data(for: r)
                }
                if let http = response as? HTTPURLResponse, http.statusCode == 409,
                   let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                   let received = (json["received"] as? NSNumber)?.int64Value {
                    // Part arrived: carry on from there.
                    offset = received
                    attempt += 1
                    if attempt > 6 { throw ClientError.server(serverMessage(status: 409, body: data)) }
                    continue
                }
                _ = try check(data, response)
                progressHandler = nil
                return
            } catch let error as URLError where error.code != .cancelled && error.code != .serverCertificateUntrusted {
                attempt += 1
                guard attempt <= 6 else { throw error }
                try await Task.sleep(nanoseconds: UInt64(min(attempt, 3)) * 1_500_000_000)
                // Ask how much arrived; if the computer can't say yet, keep trying a bit.
                if let received = try? await uploadStatus(uploadID, host: host, pairing: pairing) {
                    offset = received
                }
            }
        }
    }

    func uploadStatus(_ id: String, host: String, pairing: Pairing) async throws -> Int64 {
        var request = URLRequest(url: Self.url(host: host, port: pairing.port, path: "/v1/uploads/\(id)"), timeoutInterval: 5)
        request.setValue("Bearer \(pairing.deviceKey)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await session.data(for: request)
        let checked = try check(data, response)
        guard let json = try JSONSerialization.jsonObject(with: checked) as? [String: Any],
              let received = (json["received"] as? NSNumber)?.int64Value else { throw ClientError.badResponse }
        return received
    }

    // MARK: Inbox (computer → phone)

    /// What waits on the computer for this phone. A computer with an older Toss has no inbox: empty.
    public func inbox(host: String, pairing: Pairing) async throws -> [InboxItem] {
        var request = URLRequest(url: Self.url(host: host, port: pairing.port, path: "/v1/inbox"), timeoutInterval: 10)
        request.setValue("Bearer \(pairing.deviceKey)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await session.data(for: request)
        if (response as? HTTPURLResponse)?.statusCode == 404 { return [] }
        return try InboxItem.decodeList(check(data, response))
    }

    /// Downloads a waiting file into a temporary location and returns it.
    public func download(_ item: InboxItem, host: String, pairing: Pairing) async throws -> URL {
        var request = URLRequest(url: Self.url(host: host, port: pairing.port, path: "/v1/inbox/\(item.id)"), timeoutInterval: 30)
        request.setValue("Bearer \(pairing.deviceKey)", forHTTPHeaderField: "Authorization")
        let (temp, response) = try await session.download(for: request)
        guard let http = response as? HTTPURLResponse else { throw ClientError.badResponse }
        guard (200..<300).contains(http.statusCode) else {
            let body = (try? Data(contentsOf: temp)) ?? Data()
            throw ClientError.server(serverMessage(status: http.statusCode, body: body))
        }
        // The download's file disappears when this returns: move it somewhere of our own.
        let kept = FileManager.default.temporaryDirectory.appendingPathComponent("inbox-\(UUID().uuidString)")
        try FileManager.default.moveItem(at: temp, to: kept)
        return kept
    }

    /// Tells the computer the item arrived, so it drops it.
    public func acknowledge(_ item: InboxItem, host: String, pairing: Pairing) async throws {
        var request = URLRequest(url: Self.url(host: host, port: pairing.port, path: "/v1/inbox/\(item.id)"), timeoutInterval: 10)
        request.httpMethod = "DELETE"
        request.setValue("Bearer \(pairing.deviceKey)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await session.data(for: request)
        if (response as? HTTPURLResponse)?.statusCode == 404 { return } // someone else got it
        _ = try check(data, response)
    }
}
