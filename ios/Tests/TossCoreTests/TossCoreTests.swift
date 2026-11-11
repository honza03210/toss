import XCTest
@testable import TossCore

final class TossCoreTests: XCTestCase {
    // The exact link the Go companion's TestPairingLink produces.
    let goLink = "toss://pair?v=1&n=honza+laptop&h=honza-t490.local%2C192.168.0.5&p=47800&fp=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&t=TOK"

    func testParsesGoLink() throws {
        let link = try PairingLink(string: goLink)
        XCTAssertEqual(link.computerName, "honza laptop")
        XCTAssertEqual(link.hosts, ["honza-t490.local", "192.168.0.5"])
        XCTAssertEqual(link.port, 47800)
        XCTAssertEqual(link.token, "TOK")
        XCTAssertEqual(Base64URL.decode(link.fingerprint)?.count, 32)
    }

    func testRejectsBadLinks() {
        XCTAssertThrowsError(try PairingLink(string: "https://example.com")) { XCTAssertEqual($0 as? PairingLink.ParseError, .notTossLink) }
        XCTAssertThrowsError(try PairingLink(string: goLink.replacingOccurrences(of: "v=1", with: "v=2"))) {
            XCTAssertEqual($0 as? PairingLink.ParseError, .unsupportedVersion("2"))
        }
        XCTAssertThrowsError(try PairingLink(string: goLink.replacingOccurrences(of: "&t=TOK", with: ""))) {
            XCTAssertEqual($0 as? PairingLink.ParseError, .missing("token"))
        }
        XCTAssertThrowsError(try PairingLink(string: goLink.replacingOccurrences(of: "fp=AAAA", with: "fp=AA")))
    }

    func testBase64URLRoundTrip() {
        let data = Data((0..<32).map { UInt8($0 * 7 % 256) })
        let s = Base64URL.encode(data)
        XCTAssertFalse(s.contains("=") || s.contains("+") || s.contains("/"))
        XCTAssertEqual(Base64URL.decode(s), data)
    }

    func testHostsToTryPutsLastHostFirst() {
        var p = Pairing(computerID: "c", computerName: "n", hosts: ["a.local", "1.2.3.4", "5.6.7.8"], port: 1, fingerprint: "f", deviceKey: "k")
        XCTAssertEqual(p.hostsToTry, ["a.local", "1.2.3.4", "5.6.7.8"])
        p.lastHost = "5.6.7.8"
        XCTAssertEqual(p.hostsToTry, ["5.6.7.8", "a.local", "1.2.3.4"])
    }

    func testSharedStateUpsertAndOrder() throws {
        var s = SharedState(deviceID: "d")
        s.upsert(Pairing(computerID: "1", computerName: "old", hosts: ["h"], port: 1, fingerprint: "f", deviceKey: "k", pairedAt: Date(timeIntervalSince1970: 10)))
        s.upsert(Pairing(computerID: "2", computerName: "b", hosts: ["h"], port: 1, fingerprint: "f", deviceKey: "k", pairedAt: Date(timeIntervalSince1970: 20)))
        s.upsert(Pairing(computerID: "1", computerName: "new", hosts: ["h"], port: 1, fingerprint: "f", deviceKey: "k", pairedAt: Date(timeIntervalSince1970: 5)))
        XCTAssertEqual(s.pairings.count, 2)
        XCTAssertEqual(s.pairingsByUse.map(\.computerName), ["b", "new"])
        s.update("1") { $0.lastUsed = Date(timeIntervalSince1970: 30) }
        XCTAssertEqual(s.pairingsByUse.first?.computerName, "new")
        let back = try JSONDecoder().decode(SharedState.self, from: JSONEncoder().encode(s))
        XCTAssertEqual(back, s)
        // Older stored state without newer fields still decodes.
        let old = try JSONDecoder().decode(SharedState.self, from: Data(#"{"deviceID":"x"}"#.utf8))
        XCTAssertTrue(old.convertHEIC)
        XCTAssertEqual(old.deviceName, "iPhone")
    }

    func testHeaders() {
        let file = OutgoingItem.file(URL(fileURLWithPath: "/tmp/x"), name: "Žluťoučký kůň 1.jpg", mime: "image/jpeg")
        let h = ItemRequest.headers(for: file, deviceKey: "KEY", batch: "B", index: 1, count: 3)
        XCTAssertEqual(h["Authorization"], "Bearer KEY")
        XCTAssertEqual(h["X-Toss-Kind"], "file")
        XCTAssertEqual(h["X-Toss-Name"], "%C5%BDlu%C5%A5ou%C4%8Dk%C3%BD%20k%C5%AF%C5%88%201.jpg")
        XCTAssertEqual(h["X-Toss-Index"], "1")
        XCTAssertEqual(ItemRequest.headers(for: .text("hi"), deviceKey: "K", batch: "B", index: 0, count: 1)["X-Toss-Kind"], "text")
        XCTAssertEqual(ItemRequest.body(for: .link(URL(string: "https://a.b/c")!)), Data("https://a.b/c".utf8))
        XCTAssertNil(ItemRequest.body(for: file))
    }

    func testDedupe() {
        let url = URL(string: "https://example.com/page")!
        let items: [OutgoingItem] = [.link(url), .text("https://example.com/page"), .text(" "), .text("Note"), .text("Note")]
        XCTAssertEqual(dedupe(items), [.link(url), .text("Note")])
    }

    func testMimeAndMessages() {
        XCTAssertEqual(mimeType(forFileName: "IMG_1.HEIC"), "image/heic")
        XCTAssertEqual(mimeType(forFileName: "x.unknown"), "application/octet-stream")
        XCTAssertEqual(serverMessage(status: 403, body: Data(#"{"error":"pairing code expired"}"#.utf8)), "pairing code expired")
        XCTAssertTrue(serverMessage(status: 401, body: Data()).contains("Pair again"))
    }
}
