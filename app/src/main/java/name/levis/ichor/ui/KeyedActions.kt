package name.levis.ichor.ui

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** [runCatching] that lets a cancellation through instead of reporting it as a failure. */
internal suspend fun <T> cancellableCatching(block: suspend () -> T): Result<T> = try {
    Result.success(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Throwable) {
    Result.failure(e)
}

/**
 * Actions on keyed targets (a certificate, a volume, a Flux object), at most one in flight
 * per key, run in [scope] (a ViewModel's). [busy] holds the keys in flight; [results] queues
 * one [R] per finished action: a queue, not a state, so two finishing together each get
 * their message. A cancelled scope is not an outcome.
 */
class KeyedActions<R>(private val scope: CoroutineScope) {
    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    private val _results = Channel<R>(Channel.BUFFERED)
    val results: Flow<R> = _results.receiveAsFlow()

    /**
     * Runs [action] unless [key] is busy, sends [result] of its outcome, then [onSuccess]
     * when it went through.
     */
    fun <T> launch(key: String, action: suspend () -> T, result: (Result<T>) -> R, onSuccess: () -> Unit = {}) {
        if (key in _busy.value) return
        _busy.update { it + key }
        scope.launch {
            val outcome = cancellableCatching(action)
            _busy.update { it - key }
            _results.send(result(outcome))
            if (outcome.isSuccess) onSuccess()
        }
    }
}
