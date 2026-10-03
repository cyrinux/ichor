import UIKit
import ImageIO
import IchorCore

/// The "Download missing app icons" preference: off until the user turns it on (a third-party
/// request, see RemoteAppIcons).
enum AppIconSettings {
    static let remoteKey = "remoteAppIcons"

    static var remoteEnabled: Bool {
        get { UserDefaults.standard.bool(forKey: remoteKey) }
        set { UserDefaults.standard.set(newValue, forKey: remoteKey) }
    }
}

/// Icons bundled with the app (appicons/<name>.webp, shared with Android), with their light
/// "-night" variant for dark themes when there is one. iOS 17 decodes WebP itself.
enum BundledAppIcons {
    // NSCache is thread-safe; a miss (most icons have no -night variant) is remembered too.
    private static let images = NSCache<NSString, UIImage>()
    private static let missing = NSCache<NSString, NSNumber>()

    static func image(_ name: String, dark: Bool) -> UIImage? {
        // Defense in depth: InventoryApp.iconSource already drops names that are not slugs.
        guard isValidIconSlug(name) else { return nil }
        if dark, let night = load("\(name)-night") { return night }
        return load(name)
    }

    private static func load(_ file: String) -> UIImage? {
        let key = file as NSString
        if let image = images.object(forKey: key) { return image }
        guard missing.object(forKey: key) == nil else { return nil }
        guard let url = Bundle.main.url(forResource: file, withExtension: "webp", subdirectory: "appicons"),
              let image = UIImage(contentsOfFile: url.path) else {
            missing.setObject(NSNumber(value: true), forKey: key)
            return nil
        }
        images.setObject(image, forKey: key)
        return image
    }
}

/// Icons of recognised apps the app does not bundle, from Dashboard Icons on jsDelivr. Only
/// asked once the user allowed it, with nothing but the validated icon name in the URL (no
/// cookies, no cache shared with other requests, no redirects followed). A response over
/// remoteIconMaxBytes is refused and images are decoded at icon size only. Kept on disk in the
/// caches directory; a failure (e.g. 404) is not retried before the next launch.
actor RemoteAppIcons {
    static let shared = RemoteAppIcons()

    /// Icons are drawn at 76 pt at most: 256 px covers @3x.
    private static let maxPixels = 256

    private var images: [String: UIImage] = [:]
    private var failed: Set<String> = []
    private var loading: [String: Task<UIImage?, Never>] = [:]

    private let session: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.httpShouldSetCookies = false
        config.httpCookieAcceptPolicy = .never
        config.urlCache = nil
        config.timeoutIntervalForRequest = 15
        return URLSession(configuration: config, delegate: NoRedirects(), delegateQueue: nil)
    }()

    private static let directory: URL? = FileManager.default
        .urls(for: .cachesDirectory, in: .userDomainMask).first?
        .appendingPathComponent("appicons-remote", isDirectory: true)

    func image(slug: String) async -> UIImage? {
        if let image = images[slug] { return image }
        guard !failed.contains(slug), let url = remoteIconURL(slug: slug) else { return nil }
        if let running = loading[slug] { return await running.value }
        let task = Task { await fetch(slug: slug, url: url) }
        loading[slug] = task
        let image = await task.value
        loading[slug] = nil
        if let image { images[slug] = image } else { failed.insert(slug) }
        return image
    }

    private func fetch(slug: String, url: URL) async -> UIImage? {
        let file = Self.directory?.appendingPathComponent("\(slug).webp")
        if let file, let data = try? Data(contentsOf: file) {
            if data.count <= remoteIconMaxBytes, let image = Self.decode(data) { return image }
            try? FileManager.default.removeItem(at: file)
        }
        guard let data = await download(url), let image = Self.decode(data) else { return nil }
        if let file, let directory = Self.directory {
            try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try? data.write(to: file, options: .atomic)
        }
        return image
    }

    /// The body of a 200 answer, nil past remoteIconMaxBytes (announced or received: the
    /// download stops there).
    private func download(_ url: URL) async -> Data? {
        guard let result = try? await session.bytes(from: url),
              let http = result.1 as? HTTPURLResponse, http.statusCode == 200,
              http.expectedContentLength <= Int64(remoteIconMaxBytes) else { return nil }
        let bytes = result.0
        var data = Data()
        do {
            for try await byte in bytes {
                data.append(byte)
                if data.count > remoteIconMaxBytes {
                    bytes.task.cancel()
                    return nil
                }
            }
        } catch {
            return nil
        }
        return data
    }

    /// Decodes at most maxPixels on the longest side, whatever size the file claims.
    private static func decode(_ data: Data) -> UIImage? {
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixels,
        ]
        guard let source = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else { return nil }
        return UIImage(cgImage: image)
    }
}

/// Refuses every redirect: the icon must come from the validated jsDelivr URL itself.
private final class NoRedirects: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}
