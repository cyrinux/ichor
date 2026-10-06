import Foundation

/// Folds the outcome of an action the user launched into what its screen shows: a failure
/// becomes the message to alert with, a success bumps the counter driving the success
/// feedback. Returns whether it went through, to announce it.
@discardableResult
public func recordActionOutcome(_ failure: String?, message: inout String?, succeeded: inout Int) -> Bool {
    if let failure {
        message = failure
        return false
    }
    succeeded += 1
    return true
}
