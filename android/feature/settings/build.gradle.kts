plugins {
    id("mycorrhizal.android.library")
    id("mycorrhizal.android.hilt")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "com.mycorrhizal.crm.feature.settings"
}

dependencies {
    implementation(project(":core:data"))
    implementation(project(":core:domain"))
    implementation(project(":core:ui"))
    implementation(project(":feature:tracking"))
    // Issue #814 Phase 2: encode the TOTP enrollment otpauth:// URI as a QR
    // bitmap (pure-Java, no scanning/camera).
    implementation(libs.zxing.core)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material.icons.extended)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.hilt.android)
    implementation(libs.hilt.navigation.compose)
    ksp(libs.hilt.android.compiler)

    debugImplementation(libs.androidx.compose.ui.tooling)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
    testImplementation(libs.androidx.compose.ui.test.junit4)
    // Issue #214: the shared Compose semantics a11y-sweep assertion helper.
    testImplementation(project(":core:testing"))
}
