package com.doubleagent

import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import android.os.Build
import android.util.Log
import java.util.concurrent.LinkedBlockingQueue

class AudioPlayer {
    private var audioTrack: AudioTrack? = null
    private val audioQueue = LinkedBlockingQueue<Protocol.TimestampedData>(100)
    private var running = false
    private var thread: Thread? = null

    private var sampleRate = 48000
    private var channels = 2
    private var bits = 16

    fun configure(sampleRate: Int, channels: Int, bits: Int) {
        this.sampleRate = sampleRate
        this.channels = channels
        this.bits = bits
    }

    fun start() {
        running = true
        thread = Thread {
            try {
                val channelConfig = if (channels == 2)
                    AudioFormat.CHANNEL_OUT_STEREO else AudioFormat.CHANNEL_OUT_MONO
                val encoding = if (bits == 16)
                    AudioFormat.ENCODING_PCM_16BIT else AudioFormat.ENCODING_PCM_8BIT

                val minBufferSize = AudioTrack.getMinBufferSize(sampleRate, channelConfig, encoding)
                // 60ms buffer for smooth playback
                val bufferSize = (sampleRate * 60 / 1000 * channels * (bits / 8)).coerceAtLeast(minBufferSize)

                val attrs = AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_MEDIA)
                    .setContentType(AudioAttributes.CONTENT_TYPE_MOVIE)
                    .build()

                val format = AudioFormat.Builder()
                    .setSampleRate(sampleRate)
                    .setEncoding(encoding)
                    .setChannelMask(channelConfig)
                    .build()

                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    audioTrack = AudioTrack.Builder()
                        .setAudioAttributes(attrs)
                        .setAudioFormat(format)
                        .setBufferSizeInBytes(bufferSize)
                        .setTransferMode(AudioTrack.MODE_STREAM)
                        .setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY)
                        .build()
                } else {
                    audioTrack = AudioTrack(attrs, format, bufferSize, AudioTrack.MODE_STREAM, 0)
                }

                audioTrack?.play()
                Log.i("AudioPlayer", "Started: bufferSize=$bufferSize sampleRate=$sampleRate")

                var totalWritten = 0
                val bytesPerSample = (bits / 8) * channels

                while (running) {
                    val td = audioQueue.poll()
                    if (td != null) {
                        // If queue has built up too much (>200ms), drop oldest to reduce latency
                        val queueMs = (audioQueue.size * 20) // ~20ms per packet at 3840 bytes
                        if (queueMs > 200) {
                            val dropCount = audioQueue.size / 2
                            repeat(dropCount) { audioQueue.poll() }
                            Log.w("AudioPlayer", "Dropped $dropCount packets, queue was ${queueMs}ms")
                        }

                        val written = audioTrack?.write(td.data, 0, td.data.size) ?: 0
                        if (written > 0) {
                            totalWritten += written
                        }
                    } else {
                        Thread.sleep(1)
                    }
                }
            } catch (e: Exception) {
                Log.e("AudioPlayer", "Audio loop error", e)
            } finally {
                audioTrack?.stop()
                audioTrack?.release()
                audioTrack = null
            }
        }
        thread?.start()
    }

    fun queueAudio(td: Protocol.TimestampedData) {
        audioQueue.offer(td)
    }

    fun stop() {
        running = false
        thread?.join(1000)
    }
}
