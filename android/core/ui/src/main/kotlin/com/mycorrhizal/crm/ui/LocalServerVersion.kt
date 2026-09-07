package com.mycorrhizal.crm.ui

import androidx.compose.runtime.staticCompositionLocalOf
import com.mycorrhizal.crm.model.AppVersion

/**
 * The server version the current session resolved from GET /health (issue
 * #528), provided at the app root once the per-session compatibility check has
 * run. Screens consult it through ServerCapabilities (core:domain) to hide or
 * disable capabilities the connected server is too old to provide (issue #692).
 *
 * Null is the fail-open default: before the check has resolved, in previews, or
 * when /health was unreachable/garbled, every capability is treated as
 * available — the client must never degrade itself because it could not confirm
 * the server's version.
 */
val LocalServerVersion = staticCompositionLocalOf<AppVersion?> { null }
