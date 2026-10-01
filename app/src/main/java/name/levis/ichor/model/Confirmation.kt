package name.levis.ichor.model

/**
 * Whether [typed] confirms a risky action guarded by [token] (a hostname to type). A blank
 * token never matches: an empty field must not confirm anything.
 */
fun confirmationMatches(typed: String, token: String): Boolean = token.isNotBlank() && typed.trim() == token.trim()
