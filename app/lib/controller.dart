import 'package:flutter/foundation.dart';

import 'api.dart';
import 'settings.dart';

/// Holds the connection settings and the API client built from them.
class AppController extends ChangeNotifier {
  AppController(this.storage, {this.apiBuilder});

  final AppStorage storage;

  /// Replaceable in tests; the app keeps the screens' answers on the phone.
  final LifeApi? Function(ConnectionSettings)? apiBuilder;

  bool loaded = false;
  ConnectionSettings connection = const ConnectionSettings(baseUrl: '', token: '');
  LifeApi? api;

  Future<void> load() async {
    connection = await storage.loadConnection();
    api = await _build(connection);
    loaded = true;
    notifyListeners();
  }

  Future<void> saveConnection(ConnectionSettings c) async {
    await storage.saveConnection(c);
    connection = c;
    api = await _build(c);
    notifyListeners();
  }

  Future<LifeApi?> _build(ConnectionSettings c) async {
    final build = apiBuilder;
    return build != null ? build(c) : cachedApiFor(c);
  }
}
