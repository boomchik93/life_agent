import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';
import 'response_cache.dart';

class ConnectionSettings {
  const ConnectionSettings({required this.baseUrl, required this.token});

  final String baseUrl;
  final String token;

  bool get isComplete => baseUrl.isNotEmpty && token.isNotEmpty;
}

/// Validates the server address. Only HTTPS is allowed, except a loopback
/// address for development through `adb reverse`.
String? validateBaseUrl(String raw) {
  final s = raw.trim();
  final u = Uri.tryParse(s);
  if (s.isEmpty || u == null || !u.hasAuthority || u.host.isEmpty) {
    return 'Введи адрес сервера, например https://life.example.com';
  }
  final loopback = u.host == 'localhost' || u.host == '127.0.0.1';
  if (u.scheme == 'https' || (u.scheme == 'http' && loopback)) return null;
  return 'Нужен адрес с https://';
}

/// Where the app keeps its state.
abstract class AppStorage {
  Future<ConnectionSettings> loadConnection();
  Future<void> saveConnection(ConnectionSettings c);

  /// Date (YYYY-MM-DD) of the last saved check-in, to show the card once a day.
  Future<String?> lastCheckinDate();
  Future<void> saveCheckinDate(String ymd);

  /// Whether the background health sync is on.
  Future<bool> healthSyncEnabled();
  Future<void> saveHealthSyncEnabled(bool on);

  /// Whether the background task shows notifications with the advice.
  Future<bool> adviceNotifyEnabled();
  Future<void> saveAdviceNotifyEnabled(bool on);

  /// Whether the background task sends the phone's app-usage sums.
  Future<bool> usageSyncEnabled();
  Future<void> saveUsageSyncEnabled(bool on);

  /// When the last sync ran (ISO 8601) and why it sent nothing, if it did not.
  Future<({String? at, String? note})> lastHealthSync();
  Future<void> saveHealthSync(String at, String? note);
}

/// The token goes to the system's secure storage, the rest to ordinary preferences.
class PlatformStorage implements AppStorage {
  // On the Mac the token goes to the login keychain: the data protection
  // keychain needs an app signed by a developer team.
  PlatformStorage({FlutterSecureStorage? secure})
      : _secure = secure ?? const FlutterSecureStorage(mOptions: MacOsOptions(usesDataProtectionKeychain: false));

  final FlutterSecureStorage _secure;

  static const _kUrl = 'base_url';
  static const _kToken = 'token';
  static const _kCheckin = 'last_checkin_date';
  static const _kHealthOn = 'health_sync_on';
  static const _kAdviceOn = 'advice_notify_on';
  static const _kUsageOn = 'usage_sync_on';
  static const _kHealthAt = 'health_sync_at';
  static const _kHealthNote = 'health_sync_note';

  @override
  Future<ConnectionSettings> loadConnection() async {
    final prefs = await SharedPreferences.getInstance();
    return ConnectionSettings(
      baseUrl: prefs.getString(_kUrl) ?? '',
      token: await _secure.read(key: _kToken) ?? '',
    );
  }

  @override
  Future<void> saveConnection(ConnectionSettings c) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_kUrl, c.baseUrl);
    await _secure.write(key: _kToken, value: c.token);
  }

  @override
  Future<String?> lastCheckinDate() async =>
      (await SharedPreferences.getInstance()).getString(_kCheckin);

  @override
  Future<void> saveCheckinDate(String ymd) async =>
      (await SharedPreferences.getInstance()).setString(_kCheckin, ymd);

  @override
  Future<bool> healthSyncEnabled() async =>
      (await SharedPreferences.getInstance()).getBool(_kHealthOn) ?? false;

  @override
  Future<void> saveHealthSyncEnabled(bool on) async =>
      (await SharedPreferences.getInstance()).setBool(_kHealthOn, on);

  @override
  Future<bool> adviceNotifyEnabled() async =>
      (await SharedPreferences.getInstance()).getBool(_kAdviceOn) ?? false;

  @override
  Future<void> saveAdviceNotifyEnabled(bool on) async =>
      (await SharedPreferences.getInstance()).setBool(_kAdviceOn, on);

  @override
  Future<bool> usageSyncEnabled() async =>
      (await SharedPreferences.getInstance()).getBool(_kUsageOn) ?? false;

  @override
  Future<void> saveUsageSyncEnabled(bool on) async =>
      (await SharedPreferences.getInstance()).setBool(_kUsageOn, on);

  @override
  Future<({String? at, String? note})> lastHealthSync() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.reload(); // the background isolate wrote it
    return (at: prefs.getString(_kHealthAt), note: prefs.getString(_kHealthNote));
  }

  @override
  Future<void> saveHealthSync(String at, String? note) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_kHealthAt, at);
    if (note == null) {
      await prefs.remove(_kHealthNote);
    } else {
      await prefs.setString(_kHealthNote, note);
    }
  }
}

/// Builds the API client for the stored settings; null while they are incomplete.
LifeApi? apiFor(ConnectionSettings c, {ResponseCache? cache}) =>
    c.isComplete ? HttpLifeApi(baseUrl: c.baseUrl.trim(), token: c.token.trim(), cache: cache) : null;

/// The client of the app itself: the answers of the screens are kept on the
/// phone for the next start.
Future<LifeApi?> cachedApiFor(ConnectionSettings c) async =>
    c.isComplete ? apiFor(c, cache: await ResponseCache.open(c.baseUrl.trim())) : null;
