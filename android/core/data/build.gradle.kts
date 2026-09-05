plugins {
    id("mycorrhizal.android.library")
    id("mycorrhizal.android.hilt")
    id("com.google.devtools.ksp")
}

android {
    namespace = "com.mycorrhizal.crm.data"

    // Issue #480: exportSchema is on (AppDatabase) so Room writes a versioned
    // JSON schema here on every compile. That JSON is what `MigrationTestHelper`
    // needs to build a historical "before" database for a migration test, and
    // what `onValidateSchema` compares a migration's end state against — so the
    // committed files are the real regression net for the current and every
    // future migration, not just documentation. Exposed to both `test` (Robolectric)
    // and `androidTest` (device) source sets since either could host a
    // MigrationTestHelper-based test.
    sourceSets {
        getByName("test").assets.srcDir("schemas")
        getByName("androidTest").assets.srcDir("schemas")
    }
}

ksp {
    arg("room.schemaLocation", "$projectDir/schemas")
}

dependencies {
    api(project(":core:domain"))
    api(project(":core:network"))
    implementation(project(":core:model"))

    implementation(libs.hilt.android)
    ksp(libs.hilt.android.compiler)

    implementation(libs.room.runtime)
    implementation(libs.room.ktx)
    ksp(libs.room.compiler)

    // Issue #385: whole-DB encryption of the Room mirror via SQLCipher.
    implementation(libs.sqlcipher.android)

    implementation(libs.androidx.datastore.preferences)
    implementation(libs.androidx.security.crypto)
    // Issue #722: the device-posture check (BiometricManager) behind the
    // opt-in app-lock capability.
    implementation(libs.androidx.biometric)
    // Issue #722: ProcessLifecycleOwner drives the app-lock grace period from
    // true process background/foreground transitions.
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.moshi)
    implementation(libs.moshi.kotlin)
    ksp(libs.moshi.kotlin.codegen)

    testImplementation(libs.room.testing)
    testImplementation(libs.kotlinx.coroutines.test)
}

// Issue #385: the sqlcipher-android AAR's native libsqlcipher.so is merged
// into APKs only — a JVM unit test never sees it, so `System.loadLibrary`
// fails ("no sqlcipher in java.library.path"). Robolectric tests need the
// host-ABI .so on java.library.path (a flat dir; the AAR's ABI-subdir layout
// is an Android packaging convention the JVM does not honor). Extract the
// host ABI's .so flat into build/sqlcipher-native and expose that dir.
val hostAbi = if (System.getProperty("os.arch").contains("aarch64")) "arm64-v8a" else "x86_64"

val sqlcipherAar = configurations.detachedConfiguration(
    dependencies.create("net.zetetic:sqlcipher-android:${libs.versions.sqlcipher.get()}"),
).files.filter { it.extension == "aar" }.single()

val extractSqlcipherNative by tasks.registering(Sync::class) {
    from(zipTree(sqlcipherAar)) {
        include("jni/$hostAbi/libsqlcipher.so")
        eachFile { relativePath = RelativePath(true, name) }
    }
    into(layout.buildDirectory.dir("sqlcipher-native"))
}

tasks.withType<Test>().configureEach {
    dependsOn(extractSqlcipherNative)
    systemProperty("java.library.path", layout.buildDirectory.dir("sqlcipher-native").get().asFile.absolutePath)
}
