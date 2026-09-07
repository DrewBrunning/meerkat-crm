package com.mycorrhizal.crm.model

/**
 * A parsed client/server version: the `major[.minor[.patch]]` triple that a
 * `versionName` (or the version the server reports) is reduced to for the
 * client/server compatibility check (issue #528).
 *
 * Parsing is deliberately tolerant of everything a real deployment can throw
 * at it — a leading `v` (git-tag style), a `-prerelease` suffix (`0.7.0-rc.1`),
 * or `+build` metadata — and ignores those suffixes for the comparison: the
 * numeric triple is the compatibility decision input, matching how the
 * backend's MIN_CLIENT_VERSION floor is documented and set.
 *
 * A value that is NOT a recognisable `major[.minor[.patch]]` shape (the
 * backend reports `"dev"` for an unstamped build; a version string might be
 * garbage) parses to `null`. Callers treat `null` as "cannot decide, therefore
 * compatible" — the fail-open rule from docs/client-compatibility-policy.md.
 */
data class AppVersion(
    val major: Int,
    val minor: Int,
    val patch: Int,
) : Comparable<AppVersion> {

    override fun compareTo(other: AppVersion): Int =
        compareValuesBy(this, other, { it.major }, { it.minor }, { it.patch })

    override fun toString(): String = "$major.$minor.$patch"

    companion object {
        /**
         * Parses [raw] into an [AppVersion], or returns null when it is not a
         * recognisable `major[.minor[.patch]]` shape. See the class doc for the
         * tolerated forms and why null is the fail-open signal.
         */
        fun parse(raw: String?): AppVersion? {
            val trimmed = raw?.trim().orEmpty()
            if (trimmed.isEmpty()) return null
            // Tolerate a leading "v" (git-tag style) then cut prerelease and
            // build metadata at the first '-' or '+'.
            val core = trimmed.removePrefix("v")
                .substringBefore('-')
                .substringBefore('+')
            val segments = core.split('.')
            if (segments.size !in 1..3) return null
            val numbers = segments.map { segment ->
                if (segment.isEmpty() || !segment.all(Char::isDigit)) return null
                segment.toIntOrNull() ?: return null
            }
            return AppVersion(
                major = numbers[0],
                minor = numbers.getOrElse(1) { 0 },
                patch = numbers.getOrElse(2) { 0 },
            )
        }
    }
}
