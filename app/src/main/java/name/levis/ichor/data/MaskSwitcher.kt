package name.levis.ichor.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Switches screenshot mode so that no name read under one setting is drawn under the other:
 * [transition] is on (the screens are covered) while what was read under the previous setting
 * is dropped, the mask flips, the config summary is read again and the screens are asked to
 * reload. Repository calls wait for [awaitIdle], so none runs half-way through.
 *
 * [dropReadData] forgets what was read under the previous setting, [apply] flips the mask (Go
 * and the saved setting), [reread] parses the config summary again, [reload] drops what was
 * cached meanwhile and asks the screens to fetch again (it runs while they are still covered).
 */
class MaskSwitcher(
    private val current: () -> PrivacyMask,
    private val dropReadData: suspend () -> Unit,
    private val apply: suspend (PrivacyMask) -> Unit,
    private val reread: suspend () -> Unit,
    private val reload: suspend () -> Unit,
    private val epochs: MutableStateFlow<Int> = MutableStateFlow(0),
) {
    private val _transition = MutableStateFlow(false)

    /** True while the mask switches: the screens show a placeholder and the toggle is off. */
    val transition: StateFlow<Boolean> = _transition.asStateFlow()

    /** Bumped when a switch starts and when it ends: odd while switching (see [MaskScoped]). */
    val epoch: StateFlow<Int> = epochs.asStateFlow()

    /** One switch at a time: a second one (the extra words typed on) waits for the first. */
    private val switches = Mutex()

    /** Switches to [mask]; false when it is already the one applied. */
    suspend fun switch(mask: PrivacyMask): Boolean = switches.withLock {
        if (mask == current()) return@withLock false
        _transition.value = true
        epochs.update { it + 1 }
        try {
            dropReadData()
            apply(mask)
            reread()
            reload()
        } finally {
            // Even after a failure: the placeholder never stays on screen.
            epochs.update { it + 1 }
            _transition.value = false
        }
        true
    }

    /** Returns at once outside a switch, else once it is over. */
    suspend fun awaitIdle() {
        _transition.first { !it }
    }
}

/**
 * The switches of the app's [MaskSwitcher], process-wide like the Go mask itself: what keeps
 * streamed rows (events, followed logs, watches) drops them and restarts on each switch.
 */
object MaskEpoch {
    internal val counter = MutableStateFlow(0)
    val value: StateFlow<Int> = counter.asStateFlow()
}
