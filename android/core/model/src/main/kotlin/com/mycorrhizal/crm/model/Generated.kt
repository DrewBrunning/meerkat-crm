package com.mycorrhizal.crm.model

/**
 * JaCoCo exclusion marker for declarations that structurally cannot be
 * covered by `testDebugUnitTest` (see `docs/development/coverage.md`: Android's
 * line-level override is annotating a declaration with `@Generated`). Kept
 * dependency-free and shared via `core:model` (visible to every Android
 * module) so a reason travels with each use.
 *
 * Use sparingly and always with a reason: the repo's gate is meant to be
 * satisfied by real tests, and this is the escape hatch for Hilt-rooted
 * composables and Android-Keystore/OS-prompt code that the JVM test runner
 * structurally cannot exercise.
 */
@Retention(AnnotationRetention.BINARY)
@Target(
    AnnotationTarget.CLASS,
    AnnotationTarget.FUNCTION,
    AnnotationTarget.CONSTRUCTOR,
    AnnotationTarget.PROPERTY,
)
annotation class Generated(val reason: String = "")
