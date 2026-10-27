import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';

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
}

/// The token goes to the system's secure storage, the rest to ordinary preferences.
class PlatformStorage implements AppStorage {
  PlatformStorage({FlutterSecureStorage? secure}) : _secure = secure ?? const FlutterSecureStorage();

  final FlutterSecureStorage _secure;

  static const _kUrl = 'base_url';
  static const _kToken = 'token';
  static const _kCheckin = 'last_checkin_date';

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
}

/// Builds the API client for the stored settings; null while they are incomplete.
LifeApi? apiFor(ConnectionSettings c) =>
    c.isComplete ? HttpLifeApi(baseUrl: c.baseUrl.trim(), token: c.token.trim()) : null;
