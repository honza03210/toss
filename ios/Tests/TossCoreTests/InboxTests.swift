import Foundation
import XCTest
@testable import TossCore

final class InboxTests: XCTestCase {
    func testDecodeGoInbox() throws {
        // What the Go companion writes (time.Time marshals with nanoseconds).
        let json = #"{"items":[{"id":"NYyH","kind":"text","text":"Hello","size":5,"created":"2026-11-10T21:06:21.602548486+01:00"},{"id":"G82t","kind":"file","name":"note.txt","size":10,"mime":"text/plain","created":"2026-11-10T21:06:21Z"}]}"#
        let items = try InboxItem.decodeList(Data(json.utf8))
        XCTAssertEqual(items.count, 2)
        XCTAssertEqual(items[0].text, "Hello")
        XCTAssertEqual(items[1].name, "note.txt")
        let d = try XCTUnwrap(items[0].createdDate)
        XCTAssertEqual(d.timeIntervalSince1970, 1_794_341_181.602, accuracy: 0.01)
        XCTAssertNotNil(items[1].createdDate)
    }

    func testEXIFDate() throws {
        let withOffset = try XCTUnwrap(parseEXIFDate("2024:07:14 18:30:05", offset: "+02:00"))
        XCTAssertEqual(formatRFC3339Z(withOffset), "2024-07-14T16:30:05Z")
        let utc = TimeZone(secondsFromGMT: 0)!
        let noOffset = try XCTUnwrap(parseEXIFDate("2024:07:14 18:30:05", offset: nil, defaultZone: utc))
        XCTAssertEqual(formatRFC3339Z(noOffset), "2024-07-14T18:30:05Z")
        XCTAssertNil(parseEXIFDate("garbage", offset: nil))
    }

    func testNames() {
        XCTAssertEqual(safeLocalName("../a/b.txt"), "_a_b.txt")
        XCTAssertEqual(safeLocalName("  "), "file")
        XCTAssertEqual(uniqueName("a.jpg", attempt: 1), "a.jpg")
        XCTAssertEqual(uniqueName("a.jpg", attempt: 3), "a (3).jpg")
        XCTAssertEqual(uniqueName("README", attempt: 2), "README (2)")
    }

    private func formatRFC3339Z(_ d: Date) -> String {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f.string(from: d)
    }
}
