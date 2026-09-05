package com.mycorrhizal.crm.feature.settings

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class QrCodeEncoderTest {

    @Test
    fun `encodes an otpauth uri into a square bitmap`() {
        val bitmap = QrCodeEncoder.encode("otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP&issuer=Example", 256)

        assertNotNull(bitmap)
        assertNotNull("bitmap must be square", bitmap?.let { it.width == it.height && it.width > 0 })
    }

    @Test
    fun `blank content yields no bitmap`() {
        assertNull(QrCodeEncoder.encode(""))
        assertNull(QrCodeEncoder.encode("   "))
    }
}
