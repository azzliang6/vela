package app.vela.mail

import android.os.Bundle
import android.webkit.WebView
import androidx.activity.enableEdgeToEdge
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

class MainActivity : TauriActivity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    enableEdgeToEdge()
    super.onCreate(savedInstanceState)
  }

  override fun onWebViewCreate(webView: WebView) {
    super.onWebViewCreate(webView)

    // Android 15+ forces edge-to-edge for recent target SDKs. Some WebView
    // versions still report CSS safe-area values as 0, so inset the WebView
    // natively as well and keep all controls clear of the system bars.
    ViewCompat.setOnApplyWindowInsetsListener(webView) { view, windowInsets ->
      val safeInsets = windowInsets.getInsets(
        WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()
      )
      view.setPadding(safeInsets.left, safeInsets.top, safeInsets.right, safeInsets.bottom)
      WindowInsetsCompat.CONSUMED
    }
    ViewCompat.requestApplyInsets(webView)
  }
}
