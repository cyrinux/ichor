import Foundation

/// What a screen shows: a spinner, the data, or why there is none.
public enum LoadState<T> {
    case loading
    /// `at`: when the data came from the cluster, shown in the screen footer. `refreshError`:
    /// why the latest refresh failed, while this older data stays on screen.
    case loaded(T, at: Date, refreshError: String? = nil)
    case failed(String)
}

extension LoadState {
    public static func from(_ operation: () async throws -> T) async -> LoadState<T> {
        do { return .loaded(try await operation(), at: Date()) } catch { return .failed(error.localizedDescription) }
    }

    /// `new` (a refresh's result), except that a failed refresh keeps the data on screen and
    /// notes the error instead: the error page is only for a screen with nothing to show.
    public func refreshed(with new: LoadState<T>) -> LoadState<T> {
        if case .failed(let message) = new, case .loaded(let value, let at, _) = self {
            return .loaded(value, at: at, refreshError: message)
        }
        return new
    }
}
