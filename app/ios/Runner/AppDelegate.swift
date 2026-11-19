import CoreLocation
import Flutter
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    if let r = engineBridge.pluginRegistry.registrar(forPlugin: "SchnellWegKanaele") {
      Kanaele.register(with: r)
    }
  }
}

// Gegenstück zu MainActivity.kt (D-53): dieselben Kanäle, soweit iOS sie ohne bezahlten Account kann.
// schnellweg/standort und …/laufend über CoreLocation (nur im Vordergrund), schnellweg/teilen über die iOS-Auswahl
// „Teilen“, schnellweg/kurzbefehl als Quick Actions am App-Icon. Messgang, Ausstiegs-Erinnerung, Losgeh-Wecker,
// Sperrbildschirm und Kartendienst antworten neutral (läuft nicht / nichts zu tun); die App blendet sie aus (lib/plattform.dart).
final class Kanaele: NSObject, FlutterPlugin, FlutterStreamHandler, FlutterSceneLifeCycleDelegate, CLLocationManagerDelegate {
  private let einzel = CLLocationManager()
  private let dauer = CLLocationManager()
  private var wartend: [FlutterResult] = []
  private var frist: Timer?
  private var sink: FlutterEventSink?
  private var kurzKanal: FlutterMethodChannel?
  private var kurzOffen: [String: Any]?

  static func register(with r: FlutterPluginRegistrar) {
    let k = Kanaele()
    let m = r.messenger()
    k.einzel.delegate = k
    k.einzel.desiredAccuracy = kCLLocationAccuracyBest
    k.dauer.delegate = k
    k.dauer.desiredAccuracy = kCLLocationAccuracyBestForNavigation
    k.dauer.activityType = .otherNavigation
    k.dauer.pausesLocationUpdatesAutomatically = false

    FlutterMethodChannel(name: "schnellweg/standort", binaryMessenger: m).setMethodCallHandler { c, res in
      c.method == "holen" ? k.holen(res) : res(FlutterMethodNotImplemented)
    }
    FlutterEventChannel(name: "schnellweg/standort/laufend", binaryMessenger: m).setStreamHandler(k)
    FlutterMethodChannel(name: "schnellweg/teilen", binaryMessenger: m).setMethodCallHandler { c, res in
      guard c.method == "teilen", let text = c.arguments as? String else { return res(FlutterMethodNotImplemented) }
      k.teilen(text)
      res(nil)
    }
    let kurz = FlutterMethodChannel(name: "schnellweg/kurzbefehl", binaryMessenger: m)
    kurz.setMethodCallHandler { c, res in
      switch c.method {
      case "setzen":
        k.kurzSetzen(c.arguments as? [[String: Any]] ?? [])
        res(nil)
      case "abholen":
        res(k.kurzOffen)
        k.kurzOffen = nil
      default: res(FlutterMethodNotImplemented)
      }
    }
    k.kurzKanal = kurz
    r.addSceneDelegate(k)

    neutral(m, "schnellweg/messung", ["laeuft": false])
    neutral(m, "schnellweg/aussteigen", ["laeuft": false, "beenden": NSNull()])
    neutral(m, "schnellweg/losgehen", ["erlaubt": false, "erbitten": false, "termine": NSNull(), "stellen": NSNull(),
                                       "loeschen": NSNull(), "fertig": NSNull()])
    neutral(m, "schnellweg/sperrbildschirm", ["zeigen": NSNull()])
    neutral(m, "schnellweg/karte", ["melden": NSNull(), "beenden": NSNull()])
  }

  /// Kanal ohne iOS-Gegenstück: feste Antworten, alles andere „nicht vorhanden“ (MissingPluginException in Dart).
  private static func neutral(_ m: FlutterBinaryMessenger, _ name: String, _ antworten: [String: Any]) {
    FlutterMethodChannel(name: name, binaryMessenger: m).setMethodCallHandler { c, res in
      if let a = antworten[c.method] { res(a is NSNull ? nil : a) } else { res(FlutterMethodNotImplemented) }
    }
  }

  // MARK: Standort

  private var erlaubt: Bool {
    let s = einzel.authorizationStatus
    return s == .authorizedWhenInUse || s == .authorizedAlways
  }

  private func holen(_ res: @escaping FlutterResult) {
    // Frische letzte Position (< 60 s) reicht wie auf Android und spart den GPS-Start.
    if erlaubt, let l = einzel.location, -l.timestamp.timeIntervalSinceNow < 60, l.horizontalAccuracy >= 0 {
      return res(Kanaele.karte(l))
    }
    wartend.append(res)
    if wartend.count > 1 { return }
    switch einzel.authorizationStatus {
    case .notDetermined: einzel.requestWhenInUseAuthorization() // weiter in locationManagerDidChangeAuthorization
    case .authorizedWhenInUse, .authorizedAlways: suchen()
    default: antworten(nil)
    }
  }

  private func suchen() {
    frist?.invalidate()
    frist = Timer.scheduledTimer(withTimeInterval: 10, repeats: false) { [weak self] _ in self?.antworten(nil) }
    // Erste brauchbare Position gewinnt (WLAN/Funkzelle meist schnell, GPS genauer), wie getCurrentLocation auf Android.
    einzel.startUpdatingLocation()
  }

  private func antworten(_ l: CLLocation?) {
    frist?.invalidate()
    frist = nil
    einzel.stopUpdatingLocation()
    let alle = wartend
    wartend.removeAll()
    for r in alle { r(l.map(Kanaele.karte)) }
  }

  func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
    if m === einzel, !wartend.isEmpty, frist == nil {
      switch m.authorizationStatus {
      case .notDetermined: break
      case .authorizedWhenInUse, .authorizedAlways: suchen()
      default: antworten(nil)
      }
    }
    if m === dauer, sink != nil {
      switch m.authorizationStatus {
      case .notDetermined, .authorizedWhenInUse, .authorizedAlways: break
      default: sink?(FlutterError(code: "erlaubnis", message: nil, details: nil))
      }
    }
  }

  func locationManager(_ m: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
    if m === einzel {
      if !wartend.isEmpty, let l = locations.last(where: { $0.horizontalAccuracy >= 0 }) { antworten(l) }
      return
    }
    for l in locations where l.horizontalAccuracy >= 0 {
      // zeit: Millisekunden auf der Uhr seit Systemstart (wie elapsedRealtime), unabhängig von Uhrzeit-Umstellungen.
      let zeit = (ProcessInfo.processInfo.systemUptime + l.timestamp.timeIntervalSinceNow) * 1000
      var e = Kanaele.karte(l)
      e["zeit"] = zeit
      e["tempo"] = l.speed >= 0 ? l.speed : -1.0
      sink?(e)
    }
  }

  func locationManager(_ m: CLLocationManager, didFailWithError error: Error) {
    let code = (error as? CLError)?.code
    if code == .locationUnknown { return } // vorübergehend, CoreLocation sucht weiter
    if m === einzel {
      if !wartend.isEmpty { antworten(nil) }
    } else if code == .denied {
      sink?(FlutterError(code: "erlaubnis", message: nil, details: nil))
    }
  }

  func onListen(withArguments arguments: Any?, eventSink events: @escaping FlutterEventSink) -> FlutterError? {
    switch dauer.authorizationStatus {
    case .denied, .restricted: return FlutterError(code: "erlaubnis", message: nil, details: nil)
    case .notDetermined: dauer.requestWhenInUseAuthorization()
    default: break
    }
    sink = events
    dauer.startUpdatingLocation()
    if arguments as? Bool != false { UIApplication.shared.isIdleTimerDisabled = true }
    return nil
  }

  func onCancel(withArguments arguments: Any?) -> FlutterError? {
    dauer.stopUpdatingLocation()
    sink = nil
    UIApplication.shared.isIdleTimerDisabled = false
    return nil
  }

  private static func karte(_ l: CLLocation) -> [String: Any] {
    ["lat": l.coordinate.latitude, "lon": l.coordinate.longitude, "genau": l.horizontalAccuracy]
  }

  // MARK: Teilen

  private func teilen(_ text: String) {
    let fenster = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
      .flatMap { $0.windows }.first { $0.isKeyWindow }
    guard var vc = fenster?.rootViewController else { return }
    while let p = vc.presentedViewController { vc = p }
    let auswahl = UIActivityViewController(activityItems: [text], applicationActivities: nil)
    if let pop = auswahl.popoverPresentationController { // iPad
      pop.sourceView = vc.view
      pop.sourceRect = CGRect(x: vc.view.bounds.midX, y: vc.view.bounds.midY, width: 0, height: 0)
      pop.permittedArrowDirections = []
    }
    vc.present(auswahl, animated: true)
  }

  // MARK: Kurzbefehle (Quick Actions)

  private func kurzSetzen(_ orte: [[String: Any]]) {
    UIApplication.shared.shortcutItems = orte.prefix(4).compactMap { o in
      guard let art = o["art"] as? String, let name = o["name"] as? String, let detail = o["detail"] as? String,
            let lat = o["lat"] as? Double, let lon = o["lon"] as? Double else { return nil }
      let info: [String: NSSecureCoding] = ["art": art as NSString, "name": name as NSString, "detail": detail as NSString,
                                            "lat": NSNumber(value: lat), "lon": NSNumber(value: lon)]
      return UIApplicationShortcutItem(type: "ziel_\(lat)_\(lon)", localizedTitle: name, localizedSubtitle: nil,
                                       icon: UIApplicationShortcutIcon(systemImageName: "star.fill"), userInfo: info)
    }
  }

  private func kurzLesen(_ item: UIApplicationShortcutItem) -> Bool {
    guard let u = item.userInfo, let name = u["name"] as? String, let lat = u["lat"] as? NSNumber,
          let lon = u["lon"] as? NSNumber else { return false }
    kurzOffen = ["art": u["art"] as? String ?? "karte", "name": name, "detail": u["detail"] as? String ?? "",
                 "lat": lat.doubleValue, "lon": lon.doubleValue]
    return true
  }

  // Kaltstart über einen Kurzbefehl: Die App holt ihn mit „abholen“, sobald der Zugang steht.
  func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions?) -> Bool {
    if let item = connectionOptions?.shortcutItem { _ = kurzLesen(item) }
    return false // nur mitgelesen, Flutter richtet die Szene selbst ein
  }

  // App lief schon: wie onNewIntent auf Android.
  func windowScene(_ windowScene: UIWindowScene, performActionFor shortcutItem: UIApplicationShortcutItem,
                   completionHandler: @escaping (Bool) -> Void) -> Bool {
    let ok = kurzLesen(shortcutItem)
    if ok { kurzKanal?.invokeMethod("neu", arguments: nil) }
    completionHandler(ok)
    return true
  }
}
