import AVFoundation
import Foundation
import ImageIO
import TossCore

/// When a photo or video was taken, read from the file itself (EXIF for photos, the movie's
/// creation date for videos). Sent as X-Toss-Modified so the computer dates the file correctly.
public enum MediaDate {
    public static func captureDate(of url: URL, mime: String) async -> Date? {
        if mime.hasPrefix("image/") {
            return photoDate(url)
        }
        if mime.hasPrefix("video/") {
            let asset = AVURLAsset(url: url)
            if let item = try? await asset.load(.creationDate), let date = try? await item.load(.dateValue) {
                return date
            }
        }
        return nil
    }

    static func photoDate(_ url: URL) -> Date? {
        guard let source = CGImageSourceCreateWithURL(url as CFURL, nil),
              let props = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any] else { return nil }
        if let exif = props[kCGImagePropertyExifDictionary] as? [CFString: Any] {
            for (dateKey, offsetKey) in [(kCGImagePropertyExifDateTimeOriginal, kCGImagePropertyExifOffsetTimeOriginal),
                                         (kCGImagePropertyExifDateTimeDigitized, kCGImagePropertyExifOffsetTimeDigitized)] {
                if let value = exif[dateKey] as? String,
                   let date = parseEXIFDate(value, offset: exif[offsetKey] as? String) {
                    return date
                }
            }
        }
        if let tiff = props[kCGImagePropertyTIFFDictionary] as? [CFString: Any],
           let value = tiff[kCGImagePropertyTIFFDateTime] as? String {
            return parseEXIFDate(value, offset: nil)
        }
        return nil
    }
}
