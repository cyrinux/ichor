import Observation
import IchorCore

/// Edits staged in the cluster repository's browser for one pull request with several files:
/// in memory for this session only (lost when the app is killed), per repository and branch.
@Observable
@MainActor
final class StagedEdits {
    private(set) var basket = StagedBasket()

    func of(_ key: StagedKey) -> [StagedFile] { basket.of(key) }

    func canStage(_ key: StagedKey, path: String) -> Bool { basket.canStage(key, path: path) }

    func stage(_ key: StagedKey, _ file: StagedFile) { basket.stage(key, file) }

    func remove(_ key: StagedKey, path: String) { basket.remove(key, path: path) }

    func clear(_ key: StagedKey) { basket.clear(key) }
}
