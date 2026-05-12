package com.doubleagent

import android.media.MediaCodec
import android.media.MediaFormat
import android.util.Log
import android.view.Surface
import java.nio.ByteBuffer
import java.util.concurrent.LinkedBlockingQueue

class VideoDecoder(private val surface: Surface) {
    private var codec: MediaCodec? = null
    private val frameQueue = LinkedBlockingQueue<Protocol.TimestampedData>(60)
    private var running = false
    private var thread: Thread? = null

    private var videoWidth = 1920
    private var videoHeight = 1080
    private var initialized = false
    private val initBuffer = ArrayList<Protocol.TimestampedData>()

    fun configure(width: Int, height: Int) {
        videoWidth = width
        videoHeight = height
    }

    fun start() {
        running = true
        thread = Thread {
            try {
                while (running) {
                    val td = frameQueue.poll()
                    if (td != null) {
                        if (!initialized) {
                            initBuffer.add(td)
                            tryInitFromBuffer()
                        }
                        if (initialized) {
                            decodeFrame(td)
                        }
                    } else {
                        Thread.sleep(1)
                    }
                }
            } catch (e: Exception) {
                Log.e("VideoDecoder", "Decoder loop error", e)
            } finally {
                codec?.stop()
                codec?.release()
                codec = null
            }
        }
        thread?.start()
    }

    private fun tryInitFromBuffer() {
        var totalSize = 0
        for (b in initBuffer) totalSize += b.data.size
        val allData = ByteArray(totalSize)
        var pos = 0
        for (b in initBuffer) {
            System.arraycopy(b.data, 0, allData, pos, b.data.size)
            pos += b.data.size
        }

        val sps = extractNAL(allData, 7)
        val pps = extractNAL(allData, 8)

        if (sps == null || pps == null) {
            if (initBuffer.size > 60) {
                Log.w("VideoDecoder", "No SPS/PPS in ${initBuffer.size} frames, clearing buffer")
                initBuffer.clear()
            }
            return
        }

        Log.i("VideoDecoder", "Found SPS (${sps.size}) and PPS (${pps.size})")

        val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, videoWidth, videoHeight)
        format.setByteBuffer("csd-0", ByteBuffer.wrap(sps))
        format.setByteBuffer("csd-1", ByteBuffer.wrap(pps))

        try {
            codec = MediaCodec.createDecoderByType(MediaFormat.MIMETYPE_VIDEO_AVC)
            codec?.configure(format, surface, null, 0)
            codec?.start()
            initialized = true
            Log.i("VideoDecoder", "Decoder initialized")

            for (b in initBuffer) {
                decodeFrame(b)
            }
            initBuffer.clear()
        } catch (e: Exception) {
            Log.e("VideoDecoder", "Decoder init failed", e)
        }
    }

    private fun extractNAL(data: ByteArray, nalType: Int): ByteArray? {
        var i = 0
        while (i < data.size - 4) {
            val startCodeLen = if (data[i].toInt() == 0 && data[i + 1].toInt() == 0 &&
                data[i + 2].toInt() == 0 && data[i + 3].toInt() == 1) {
                4
            } else if (data[i].toInt() == 0 && data[i + 1].toInt() == 0 &&
                data[i + 2].toInt() == 1) {
                3
            } else {
                i++
                continue
            }

            val nalHeaderOffset = i + startCodeLen
            if (nalHeaderOffset >= data.size) break

            val nalUnitType = data[nalHeaderOffset].toInt() and 0x1F
            if (nalUnitType == nalType) {
                var end = nalHeaderOffset + 1
                while (end < data.size - 3) {
                    if (data[end].toInt() == 0 && data[end + 1].toInt() == 0 &&
                        (data[end + 2].toInt() == 1 || (data[end + 2].toInt() == 0 && end + 3 < data.size && data[end + 3].toInt() == 1))) {
                        break
                    }
                    end++
                }
                if (end > data.size) end = data.size
                val nal = ByteArray(end - i)
                System.arraycopy(data, i, nal, 0, end - i)
                return nal
            }

            i += startCodeLen + 1
        }
        return null
    }

    private fun decodeFrame(td: Protocol.TimestampedData) {
        val c = codec ?: return
        try {
            val inputBufferId = c.dequeueInputBuffer(10000)
            if (inputBufferId >= 0) {
                val inputBuffer = c.getInputBuffer(inputBufferId) ?: return
                inputBuffer.clear()
                inputBuffer.put(td.data)
                c.queueInputBuffer(inputBufferId, 0, td.data.size, td.timestampUs, 0)
            }

            val bufferInfo = MediaCodec.BufferInfo()
            var outputBufferId = c.dequeueOutputBuffer(bufferInfo, 10000)
            while (outputBufferId >= 0) {
                c.releaseOutputBuffer(outputBufferId, true)
                outputBufferId = c.dequeueOutputBuffer(bufferInfo, 0)
            }
        } catch (e: Exception) {
            Log.e("VideoDecoder", "Decode error", e)
        }
    }

    fun queueFrame(td: Protocol.TimestampedData) {
        frameQueue.offer(td)
    }

    fun stop() {
        running = false
        thread?.join(1000)
    }
}
