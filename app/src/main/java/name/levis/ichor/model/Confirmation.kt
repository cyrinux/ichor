package name.levis.ichor.model

/** Names longer than this are confirmed by their last [CONFIRMATION_CODE_LENGTH] characters. */
private const val SHORT_NAME_MAX = 12
private const val CONFIRMATION_CODE_LENGTH = 5

/**
 * What the user types to confirm an action on [token]: the name itself, or only its end when it
 * is long (a Longhorn `pvc-<uuid>` volume, a generated pod name), where typing it all is a chore.
 */
fun confirmationCode(token: String): String = token.trim().let { if (it.length > SHORT_NAME_MAX) it.takeLast(CONFIRMATION_CODE_LENGTH) else it }

/**
 * Whether [typed] confirms a risky action guarded by [token] (a hostname to type): the whole
 * token, or its [confirmationCode]. A blank token never matches: an empty field must not
 * confirm anything.
 */
fun confirmationMatches(typed: String, token: String): Boolean =
    token.isNotBlank() && typed.trim().let { it == token.trim() || it == confirmationCode(token) }
