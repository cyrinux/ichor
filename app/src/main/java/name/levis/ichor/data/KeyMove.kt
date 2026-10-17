package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import name.levis.ichor.security.SecurityKeyRequiredException

/** How a move of the config keys into StrongBox went (see [ConfigRepository.moveKeyToStrongBox]). */
enum class KeyMoveResult {
    /** Every stored config is under a StrongBox key. */
    MOVED,

    /** At least one key moved, another is still out of the chip. */
    PARTIAL,

    /** No key moved: the chip refused (or did not answer), nothing changed. */
    REFUSED,

    /** A file is sealed with a security key and none was tapped this run: nothing changed for it. */
    SECURITY_KEY_REQUIRED,
}

/** One store whose key may move: its key's level (null: nothing stored; may throw) and the move. */
internal class MovableKey(val protection: () -> KeyProtection?, val move: suspend () -> Boolean)

/**
 * Moves each key of [stores] that is not known to be in StrongBox, then tells how it went from
 * the levels read again. A level that cannot be read counts as not moved, never as no key; a
 * store with nothing stored is moved too, which only drops a stray key, and counts as done.
 */
internal suspend fun moveKeys(stores: List<MovableKey>): KeyMoveResult {
    val before = stores.map { runCatching(it.protection) }
    var needsSecurityKey = false
    stores.forEachIndexed { i, store ->
        if (before[i].getOrNull() == KeyProtection.STRONGBOX) return@forEachIndexed
        try {
            store.move()
        } catch (e: CancellationException) {
            throw e
        } catch (_: SecurityKeyRequiredException) {
            needsSecurityKey = true
        } catch (_: Exception) {
            // Not moved: told by the level read again below.
        }
    }
    val after = stores.map { runCatching(it.protection) }
    return when {
        after.all(::done) -> KeyMoveResult.MOVED
        needsSecurityKey -> KeyMoveResult.SECURITY_KEY_REQUIRED
        after.indices.any { done(after[it]) && !done(before[it]) } -> KeyMoveResult.PARTIAL
        else -> KeyMoveResult.REFUSED
    }
}

/** In StrongBox, or nothing stored; a level that could not be read is not done. */
private fun done(level: Result<KeyProtection?>): Boolean =
    level.isSuccess && level.getOrNull().let { it == null || it == KeyProtection.STRONGBOX }

/** The weakest of [levels] (null ones: nothing stored), or null when none is known. */
internal fun weakest(levels: List<KeyProtection?>): KeyProtection? = levels.filterNotNull().maxByOrNull { it.ordinal }
