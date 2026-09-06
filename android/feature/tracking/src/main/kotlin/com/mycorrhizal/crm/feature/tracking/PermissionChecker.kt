package com.mycorrhizal.crm.feature.tracking

import android.content.Context
import android.content.pm.PackageManager
import androidx.core.content.ContextCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject

/**
 * Issue #721: a seam for querying OS runtime-permission grant state so the
 * Settings ViewModel (which owns the toggle -> grant -> enable flow) stays
 * framework-free under its plain-JVM unit tests. The production impl reads
 * the real [ContextCompat.checkSelfPermission]; tests inject a mock.
 */
interface PermissionChecker {
    fun isGranted(permission: String): Boolean
}

class AndroidPermissionChecker @Inject constructor(
    @ApplicationContext private val context: Context,
) : PermissionChecker {
    override fun isGranted(permission: String): Boolean =
        ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
}
