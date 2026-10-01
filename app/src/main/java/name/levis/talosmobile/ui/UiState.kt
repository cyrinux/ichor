package name.levis.talosmobile.ui

/** Load state for a screen; [Loaded.refreshing] keeps old data visible during pull-to-refresh. */
sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>
    data class Loaded<T>(val data: T, val refreshing: Boolean = false) : UiState<T>
    data class Failed(val message: String) : UiState<Nothing>
}

fun Throwable.userMessage(): String = message?.takeIf { it.isNotBlank() } ?: javaClass.simpleName
