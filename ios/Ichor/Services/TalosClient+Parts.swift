import Foundation
import Ichorgo

extension TalosClient {
    /// The Ichor site's spare-parts page for one part (kind system, memory or disk), the part's
    /// maker, model and serial after the #; nil when there is nothing to look up. Opened in the
    /// browser: the app contacts no vendor.
    static func partsLookupURL(kind: String, manufacturer: String, model: String, serial: String) -> URL? {
        let link = IchorgoPartsLookupLink(kind, manufacturer, model, serial)
        return link.isEmpty ? nil : URL(string: link)
    }
}
