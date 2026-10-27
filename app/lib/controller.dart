import 'package:flutter/foundation.dart';

import 'api.dart';
import 'settings.dart';

/// Holds the connection settings and the API client built from them.
class AppController extends ChangeNotifier {
  AppController(this.storage, {LifeApi? Function(ConnectionSettings)? apiBuilder})
      : _apiBuilder = apiBuilder ?? apiFor;

  final AppStorage storage;
  final LifeApi? Function(ConnectionSettings) _apiBuilder;

  bool loaded = false;
  ConnectionSettings connection = const ConnectionSettings(baseUrl: '', token: '');
  LifeApi? api;

  Future<void> load() async {
    connection = await storage.loadConnection();
    api = _apiBuilder(connection);
    loaded = true;
    notifyListeners();
  }

  Future<void> saveConnection(ConnectionSettings c) async {
    await storage.saveConnection(c);
    connection = c;
    api = _apiBuilder(c);
    notifyListeners();
  }
}
