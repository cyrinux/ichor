package name.levis.ichor.model

/**
 * The size the user picks for dense monospace text (logs, packets), as a factor of the theme's
 * size: never below [MIN_SP], at most [MAX_FACTOR] times the theme size.
 */
object MonoTextScale {
    const val MIN_SP = 10f
    const val MAX_FACTOR = 2f
    const val STEP = 0.15f

    fun clamp(factor: Float, baseSp: Float): Float = factor.coerceIn(minOf(MIN_SP / baseSp, 1f), MAX_FACTOR)

    fun larger(factor: Float, baseSp: Float): Float = clamp(factor + STEP, baseSp)

    fun smaller(factor: Float, baseSp: Float): Float = clamp(factor - STEP, baseSp)
}
