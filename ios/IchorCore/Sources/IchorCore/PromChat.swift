import Foundation

// The panel assistant of the Metrics screen: the Go core keeps the conversation and checks
// every proposed query against the source (go/ichorgo/prom_chat.go); this is what the app
// shows of it.

/// A panel the assistant proposed, as the Go core checked it (prom_chat.go promSuggestion).
public struct PanelSuggestion: Decodable, Equatable, Sendable {
    public var title: String
    public var query: String
    public var unit: String
    public var legend: String
    /// The source accepted the query.
    public var verified: Bool
    /// The source answered no series (a valid query about a metric that is not there).
    public var empty: Bool
    /// How many answers it took: 1 when the first query passed.
    public var attempts: Int
    /// Why the query was not checked, or its last error when the fixes ran out.
    public var notice: String

    public init(title: String = "", query: String = "", unit: String = "", legend: String = "",
                verified: Bool = false, empty: Bool = false, attempts: Int = 1, notice: String = "") {
        self.title = title
        self.query = query
        self.unit = unit
        self.legend = legend
        self.verified = verified
        self.empty = empty
        self.attempts = attempts
        self.notice = notice
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(title: try c.field(.title, ""), query: try c.field(.query, ""), unit: try c.field(.unit, ""),
                  legend: try c.field(.legend, ""), verified: try c.field(.verified, false), empty: try c.field(.empty, false),
                  attempts: try c.field(.attempts, 1), notice: try c.field(.notice, ""))
    }

    private enum CodingKeys: String, CodingKey { case title, query, unit, legend, verified, empty, attempts, notice }

    /// The panel to edit or save, under `id` ("" for a new one).
    public func panel(id: String) -> PromPanel {
        PromPanel(id: id, title: title, query: query, unit: unit, legend: legend)
    }
}

/// One bubble of the conversation: the user's message, or the model's answer with the panel it proposed.
public struct PanelChatMessage: Identifiable, Equatable, Sendable {
    public let id: UUID
    public let fromUser: Bool
    public var text: String
    public var panel: PanelSuggestion?
    /// Why the answer stopped early; `text` may still hold the part received.
    public var error: String?

    public init(fromUser: Bool, text: String, panel: PanelSuggestion? = nil, error: String? = nil) {
        id = UUID()
        self.fromUser = fromUser
        self.text = text
        self.panel = panel
        self.error = error
    }
}

/// The panel being edited, for Go ({title,query,unit,legend}): never its id.
public func promChatPanelJSON(_ panel: PromPanel) throws -> String {
    try TalosJSON.encode(PromChatPanel(title: panel.title, query: panel.query, unit: panel.unit, legend: panel.legend))
}

private struct PromChatPanel: Encodable {
    let title: String
    let query: String
    let unit: String
    let legend: String
}
