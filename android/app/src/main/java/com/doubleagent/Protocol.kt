package com.doubleagent

object Protocol {
    const val MSG_VIDEO_INFO: Byte = 0x01
    const val MSG_VIDEO_FRAME: Byte = 0x02
    const val MSG_AUDIO_INFO: Byte = 0x03
    const val MSG_AUDIO_PACKET: Byte = 0x04
    const val MSG_TOUCH_EVENT: Byte = 0x05
    const val MSG_HEARTBEAT: Byte = 0x06

    const val TOUCH_DOWN: Byte = 0x00
    const val TOUCH_MOVE: Byte = 0x01
    const val TOUCH_UP: Byte = 0x02

    data class VideoInfo(val width: Int, val height: Int, val fps: Int)
    data class AudioInfo(val sampleRate: Int, val channels: Int, val bits: Int)
    data class TouchEvent(val x: Int, val y: Int, val action: Byte, val pointerId: Byte)
    data class TimestampedData(val timestampUs: Long, val data: ByteArray)

    fun encodeVideoInfo(v: VideoInfo): ByteArray {
        return byteArrayOf(
            (v.width shr 8).toByte(), v.width.toByte(),
            (v.height shr 8).toByte(), v.height.toByte(),
            v.fps.toByte()
        )
    }

    fun decodeVideoInfo(data: ByteArray): VideoInfo {
        require(data.size >= 5)
        val width = ((data[0].toInt() and 0xFF) shl 8) or (data[1].toInt() and 0xFF)
        val height = ((data[2].toInt() and 0xFF) shl 8) or (data[3].toInt() and 0xFF)
        val fps = data[4].toInt() and 0xFF
        return VideoInfo(width, height, fps)
    }

    fun encodeAudioInfo(a: AudioInfo): ByteArray {
        return byteArrayOf(
            (a.sampleRate shr 24).toByte(), (a.sampleRate shr 16).toByte(),
            (a.sampleRate shr 8).toByte(), a.sampleRate.toByte(),
            a.channels.toByte(), a.bits.toByte()
        )
    }

    fun decodeAudioInfo(data: ByteArray): AudioInfo {
        require(data.size >= 6)
        val sampleRate = ((data[0].toInt() and 0xFF) shl 24) or
                ((data[1].toInt() and 0xFF) shl 16) or
                ((data[2].toInt() and 0xFF) shl 8) or
                (data[3].toInt() and 0xFF)
        val channels = data[4].toInt() and 0xFF
        val bits = data[5].toInt() and 0xFF
        return AudioInfo(sampleRate, channels, bits)
    }

    fun encodeTouchEvent(t: TouchEvent): ByteArray {
        return byteArrayOf(
            (t.x shr 8).toByte(), t.x.toByte(),
            (t.y shr 8).toByte(), t.y.toByte(),
            t.action, t.pointerId
        )
    }

    fun decodeTouchEvent(data: ByteArray): TouchEvent {
        require(data.size >= 6)
        val x = ((data[0].toInt() and 0xFF) shl 8) or (data[1].toInt() and 0xFF)
        val y = ((data[2].toInt() and 0xFF) shl 8) or (data[3].toInt() and 0xFF)
        val action = data[4]
        val pointerId = data[5]
        return TouchEvent(x, y, action, pointerId)
    }

    fun extractTimestamp(data: ByteArray): TimestampedData {
        require(data.size >= 8)
        val ts = ((data[0].toLong() and 0xFF) shl 56) or
                ((data[1].toLong() and 0xFF) shl 48) or
                ((data[2].toLong() and 0xFF) shl 40) or
                ((data[3].toLong() and 0xFF) shl 32) or
                ((data[4].toLong() and 0xFF) shl 24) or
                ((data[5].toLong() and 0xFF) shl 16) or
                ((data[6].toLong() and 0xFF) shl 8) or
                (data[7].toLong() and 0xFF)
        val payload = ByteArray(data.size - 8)
        System.arraycopy(data, 8, payload, 0, data.size - 8)
        return TimestampedData(ts / 1000, payload) // ns to us
    }

    fun writeMessage(output: java.io.OutputStream, type: Byte, payload: ByteArray) {
        val frame = ByteArray(5 + payload.size)
        frame[0] = type
        frame[1] = (payload.size shr 24).toByte()
        frame[2] = (payload.size shr 16).toByte()
        frame[3] = (payload.size shr 8).toByte()
        frame[4] = payload.size.toByte()
        System.arraycopy(payload, 0, frame, 5, payload.size)
        output.write(frame)
        output.flush()
    }

    fun readMessage(input: java.io.InputStream): Pair<Byte, ByteArray>? {
        val header = ByteArray(5)
        var read = 0
        while (read < 5) {
            val n = input.read(header, read, 5 - read)
            if (n == -1) return null
            read += n
        }
        val type = header[0]
        val length = ((header[1].toInt() and 0xFF) shl 24) or
                ((header[2].toInt() and 0xFF) shl 16) or
                ((header[3].toInt() and 0xFF) shl 8) or
                (header[4].toInt() and 0xFF)
        if (length > 16 * 1024 * 1024) return null
        if (length == 0) return type to ByteArray(0)
        val payload = ByteArray(length)
        read = 0
        while (read < length) {
            val n = input.read(payload, read, length - read)
            if (n == -1) return null
            read += n
        }
        return type to payload
    }
}
