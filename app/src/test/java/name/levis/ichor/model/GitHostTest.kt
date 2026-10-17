package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class GitHostTest {

    @Test
    fun hostsAreKeyedWithoutSchemeSlashOrCase() {
        assertEquals("github.com", normalizeGitHost("https://github.com/"))
        assertEquals("github.com", normalizeGitHost("  GitHub.com  "))
        assertEquals("gitlab.example.org", normalizeGitHost("http://GitLab.Example.org//"))
        assertEquals("git.example.org:3000", normalizeGitHost("https://git.example.org:3000/api/v1/"))
        assertEquals("gitlab.example.org/gitlab", normalizeGitHost("https://gitlab.example.org/gitlab/api/v4"))
        assertEquals("git.example.org", normalizeGitHost("https://user@git.example.org"))
        assertEquals("", normalizeGitHost(" https:// "))
    }

    @Test
    fun hostJsonCarriesKindAddressAndToken() {
        val json = parse(gitHostJson(GitHost("https://gitlab.example.org/", GitHostKind.GITLAB), " glpat-test ", demo = false))
        assertEquals(setOf("kind", "baseURL", "token"), json.keys)
        assertEquals("gitlab", json.string("kind"))
        assertEquals("https://gitlab.example.org", json.string("baseURL"))
        assertEquals("glpat-test", json.string("token"))
    }

    @Test
    fun autoIsLeftToTheGoCore() {
        assertEquals("auto", parse(gitHostJson(GitHost("github.com"), "t", demo = false)).string("kind"))
    }

    @Test
    fun aTokenWithQuotesIsEscaped() {
        assertEquals("a\"b\\c", parse(gitHostJson(GitHost("git.example.org", GitHostKind.GITEA), "a\"b\\c", demo = false)).string("token"))
    }

    @Test
    fun theDemoSendsNoTokenAndNoAddress() {
        assertEquals("""{"kind":"demo"}""", gitHostJson(GitHost("github.com"), "secret", demo = true))
    }

    @Test
    fun theHostNeverPrintsAToken() {
        assertEquals("GitHost(host=github.com, kind=GITHUB)", GitHost("github.com", GitHostKind.GITHUB).toString())
    }

    @Test
    fun kindsRoundTripTheirWireName() {
        GitHostKind.entries.forEach { assertEquals(it, GitHostKind.fromWire(it.wire)) }
        assertEquals(GitHostKind.AUTO, GitHostKind.fromWire("bitbucket"))
        assertFalse(GitHostKind.entries.any { it != GitHostKind.AUTO && it.label == null })
    }

    private fun parse(json: String): JsonObject = Json.parseToJsonElement(json).jsonObject

    private fun JsonObject.string(key: String): String = getValue(key).jsonPrimitive.content
}
