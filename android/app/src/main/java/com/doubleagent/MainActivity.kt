package com.doubleagent

import android.app.Activity
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.View
import android.widget.Toast

class MainActivity : Activity() {
    private lateinit var surfaceView: SurfaceView
    private var videoDecoder: VideoDecoder? = null
    private var audioPlayer: AudioPlayer? = null
    private var networkClient: NetworkClient? = null
    private val handler = Handler(Looper.getMainLooper())

    private var videoConfigured = false
    private var audioConfigured = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        surfaceView = findViewById(R.id.surfaceView)

        surfaceView.holder.addCallback(object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                startClient()
            }

            override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
            }

            override fun surfaceDestroyed(holder: SurfaceHolder) {
                stopClient()
            }
        })

        window.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    }

    private fun startClient() {
        val surface = surfaceView.holder.surface ?: return

        videoDecoder = VideoDecoder(surface)
        audioPlayer = AudioPlayer()

        networkClient = NetworkClient(
            host = "localhost",
            port = 7777,
            onVideoInfo = { info ->
                handler.post {
                    videoDecoder?.configure(info.width, info.height)
                    if (!videoConfigured) {
                        videoConfigured = true
                        videoDecoder?.start()
                        checkReady()
                    }
                }
            },
            onAudioInfo = { info ->
                handler.post {
                    audioPlayer?.configure(info.sampleRate, info.channels, info.bits)
                    if (!audioConfigured) {
                        audioConfigured = true
                        audioPlayer?.start()
                        checkReady()
                    }
                }
            },
            onVideoFrame = { payload ->
                try {
                    val td = Protocol.extractTimestamp(payload)
                    videoDecoder?.queueFrame(td)
                } catch (e: Exception) {
                    // Fallback: no timestamp
                    videoDecoder?.queueFrame(Protocol.TimestampedData(0, payload))
                }
            },
            onAudioPacket = { payload ->
                try {
                    val td = Protocol.extractTimestamp(payload)
                    audioPlayer?.queueAudio(td)
                } catch (e: Exception) {
                    audioPlayer?.queueAudio(Protocol.TimestampedData(0, payload))
                }
            },
            onConnected = {
                handler.post {
                    Toast.makeText(this, "Connected to PC", Toast.LENGTH_SHORT).show()
                }
            },
            onDisconnected = {
                handler.post {
                    Toast.makeText(this, "Disconnected", Toast.LENGTH_SHORT).show()
                    videoConfigured = false
                    audioConfigured = false
                }
            }
        )

        networkClient?.start()
    }

    private fun checkReady() {
        if (videoConfigured && audioConfigured) {
            Toast.makeText(this, "Streaming started", Toast.LENGTH_SHORT).show()
        }
    }

    private fun stopClient() {
        networkClient?.stop()
        videoDecoder?.stop()
        audioPlayer?.stop()
        networkClient = null
        videoDecoder = null
        audioPlayer = null
        videoConfigured = false
        audioConfigured = false
    }

    override fun onDestroy() {
        super.onDestroy()
        stopClient()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) {
            window.decorView.systemUiVisibility = (
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                or View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                or View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                or View.SYSTEM_UI_FLAG_FULLSCREEN
            )
        }
    }
}
