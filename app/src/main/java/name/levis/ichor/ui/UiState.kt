package name.levis.ichor.ui

/** Load state for a screen; [Loaded.refreshing] keeps old data visible during pull-to-refresh. */
sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>
    /**
     * [fetchedAt]: when [data] came from the cluster (epoch millis), shown in the screen footer.
     * [error]: why the last refresh failed while [data] stayed on screen; null once one succeeds.
     */
    data class Loaded<T>(
        val data: T,
        val refreshing: Boolean = false,
        val fetchedAt: Long = System.currentTimeMillis(),
        val error: UiText? = null,
    ) : UiState<T>
    data class Failed(val message: UiText) : UiState<Nothing>
}

/**
 * The state after a refresh failed with [message]: the data on screen stays (or the last known
 * [fallback], fetched at its epoch millis), marked with the error; [UiState.Failed] only when
 * there is nothing to show.
 */
fun <T> UiState<T>.refreshFailed(message: UiText, fallback: Pair<T, Long>? = null): UiState<T> = when {
    this is UiState.Loaded -> copy(refreshing = false, error = message)
    fallback != null -> UiState.Loaded(fallback.first, fetchedAt = fallback.second, error = message)
    else -> UiState.Failed(message)
}

fun Throwable.userMessage(): String = message?.takeIf { it.isNotBlank() } ?: javaClass.simpleName
