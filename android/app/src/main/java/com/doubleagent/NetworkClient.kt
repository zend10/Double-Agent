package com.doubleagent

import android.util.Log
import java.net.Socket
import java.util.concurrent.atomic.AtomicBoolean

class NetworkClient(
    private val host: String,
    private val port: Int,
    private val onVideoInfo: (Protocol.VideoInfo) -> Unit,
    private val onAudioInfo: (Protocol.AudioInfo) -> Unit,
    private val onVideoFrame: (ByteArray) -> Unit,
    private val onAudioPacket: (ByteArray) -> Unit,
    private val onConnected: () -> Unit,
    private val onDisconnected: () -> Unit
) {
    private var socket: Socket? = null
    private val running = AtomicBoolean(false)
    private var thread: Thread? = null

    fun start() {
        running.set(true)
        thread = Thread {
            while (running.get()) {
                try {
                    connect()
                } catch (e: Exception) {
                    Log.e("NetworkClient", "Connection error", e)
                    onDisconnected()
                }
                if (running.get()) {
                    Thread.sleep(1000)
                }
            }
        }
        thread?.start()
    }

    private fun connect() {
        Log.i("NetworkClient", "Connecting to $host:$port")
        socket = Socket(host, port)
        socket?.tcpNoDelay = true
        val input = socket?.getInputStream() ?: return

        Log.i("NetworkClient", "Connected")
        onConnected()

        while (running.get()) {
            val msg = Protocol.readMessage(input) ?: break
            when (msg.first) {
                Protocol.MSG_VIDEO_INFO -> {
                    val info = Protocol.decodeVideoInfo(msg.second)
                    Log.i("NetworkClient", "Video info: ${info.width}x${info.height}@${info.fps}")
                    onVideoInfo(info)
                }
                Protocol.MSG_AUDIO_INFO -> {
                    val info = Protocol.decodeAudioInfo(msg.second)
                    Log.i("NetworkClient", "Audio info: ${info.sampleRate}Hz ${info.channels}ch ${info.bits}bit")
                    onAudioInfo(info)
                }
                Protocol.MSG_VIDEO_FRAME -> {
                    onVideoFrame(msg.second)
                }
                Protocol.MSG_AUDIO_PACKET -> {
                    onAudioPacket(msg.second)
                }
                else -> {
                    Log.w("NetworkClient", "Unknown message type: ${msg.first}")
                }
            }
        }
    }

    fun stop() {
        running.set(false)
        try {
            socket?.close()
        } catch (e: Exception) {
            // ignore
        }
        thread?.join(1000)
    }
}
