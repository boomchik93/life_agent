import 'package:flutter/material.dart';

import '../api.dart';
import '../background.dart';
import '../controller.dart';
import '../health.dart';
import '../usage.dart';
import '../settings.dart';
import 'health_screen.dart';
import 'sources_screen.dart';
import 'models_screen.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.controller, this.healthSource, this.scheduler, this.usage});

  final AppController controller;

  /// Replaceable in tests; the phone uses Health Connect and WorkManager.
  final HealthSource? healthSource;
  final SyncScheduler? scheduler;
  final UsageSource? usage;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final _url = TextEditingController(text: widget.controller.connection.baseUrl);
  late final _token = TextEditingController(text: widget.controller.connection.token);
  final _form = GlobalKey<FormState>();
  bool _hideToken = true;
  bool _busy = false;
  String? _result;
  bool _resultOk = false;

  @override
  void dispose() {
    _url.dispose();
    _token.dispose();
    super.dispose();
  }

  ConnectionSettings get _typed => ConnectionSettings(baseUrl: _url.text.trim(), token: _token.text.trim());

  Future<void> _check() async {
    if (!_form.currentState!.validate()) return;
    setState(() {
      _busy = true;
      _result = null;
    });
    try {
      await apiFor(_typed)!.ping();
      _resultOk = true;
      _result = 'Соединение работает.';
    } on ApiException catch (e) {
      _resultOk = false;
      _result = e.message;
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    await widget.controller.saveConnection(_typed);
    if (mounted) Navigator.of(context).pop(true);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Настройки')),
      body: Form(
        key: _form,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            TextFormField(
              controller: _url,
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: const InputDecoration(
                labelText: 'Адрес сервера',
                hintText: 'https://life.example.com',
                border: OutlineInputBorder(),
              ),
              validator: (v) => validateBaseUrl(v ?? ''),
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: _token,
              obscureText: _hideToken,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(
                labelText: 'Токен',
                border: const OutlineInputBorder(),
                suffixIcon: IconButton(
                  tooltip: _hideToken ? 'Показать токен' : 'Скрыть токен',
                  icon: Icon(_hideToken ? Icons.visibility : Icons.visibility_off),
                  onPressed: () => setState(() => _hideToken = !_hideToken),
                ),
              ),
              validator: (v) => (v ?? '').trim().isEmpty ? 'Введи токен' : null,
            ),
            const SizedBox(height: 8),
            Text('Токен лежит в защищённом хранилище телефона.',
                style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: _busy ? null : _check,
                    child: _busy
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Text('Проверить'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(child: FilledButton(onPressed: _busy ? null : _save, child: const Text('Сохранить'))),
              ],
            ),
            if (_result != null)
              Padding(
                padding: const EdgeInsets.only(top: 16),
                child: Row(children: [
                  Icon(_resultOk ? Icons.check_circle : Icons.error,
                      color: _resultOk ? Colors.green : Theme.of(context).colorScheme.error),
                  const SizedBox(width: 8),
                  Expanded(child: Text(_result!)),
                ]),
              ),
            const Divider(height: 40),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.psychology),
              title: const Text('Модели'),
              subtitle: Text(widget.controller.api == null
                  ? 'Сначала сохрани адрес и токен'
                  : 'Разбор фраз, совет, резервные модели, остаток запросов'),
              trailing: const Icon(Icons.chevron_right),
              enabled: widget.controller.api != null,
              onTap: () => Navigator.of(context).push(
                MaterialPageRoute(builder: (_) => ModelsScreen(api: widget.controller.api!)),
              ),
            ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.insights),
              title: const Text('Источники'),
              subtitle: Text(widget.controller.api == null
                  ? 'Сначала сохрани адрес и токен'
                  : 'Время в приложениях, Steam, GitHub, погода; экспорт и удаление'),
              trailing: const Icon(Icons.chevron_right),
              enabled: widget.controller.api != null,
              onTap: () => Navigator.of(context).push(
                MaterialPageRoute(
                  builder: (_) => SourcesScreen(
                    api: widget.controller.api!,
                    storage: widget.controller.storage,
                    usage: widget.usage ?? PluginUsageSource(),
                    scheduler: widget.scheduler ?? WorkmanagerScheduler(),
                  ),
                ),
              ),
            ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.favorite),
              title: const Text('Здоровье'),
              subtitle: Text(widget.controller.api == null
                  ? 'Сначала сохрани адрес и токен'
                  : 'Сон и пульс из Health Connect, фоновая отправка'),
              trailing: const Icon(Icons.chevron_right),
              enabled: widget.controller.api != null,
              onTap: () => Navigator.of(context).push(
                MaterialPageRoute(
                  builder: (_) => HealthScreen(
                    api: widget.controller.api!,
                    storage: widget.controller.storage,
                    source: widget.healthSource ?? HealthConnectSource(),
                    scheduler: widget.scheduler ?? WorkmanagerScheduler(),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
