package name.levis.ichor.ui.insights

import name.levis.ichor.model.DriftChange

/** The baseline was written: its time and the differences against it. */
internal fun InsightsState.baselineSaved(at: Long?, changes: List<DriftChange>): InsightsState =
    copy(baselineAt = at, changes = changes, baselineBusy = false, error = null)

/** The baseline is gone: [changes] are against the peers again. */
internal fun InsightsState.baselineDeleted(changes: List<DriftChange>): InsightsState =
    copy(baselineAt = null, changes = changes, baselineBusy = false, error = null)

/** The write failed: the baseline and the differences on screen stay. */
internal fun InsightsState.baselineFailed(message: String): InsightsState =
    copy(baselineBusy = false, error = message)
