import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../background.dart';
import '../settings.dart';
import '../sources.dart';
import '../usage.dart';
import '../widgets/error_view.dart';

/// Where the activity data comes from: a switch, the last sync and the data of
/// every source; the phone's usage access; export and deleting everything.
class SourcesScreen extends StatefulWidget {
  const SourcesScreen({
    super.key,
    required this.api,
    required this.storage,
    required this.usage,
    required this.scheduler,
    this.now,
  });

  final LifeApi api;
  final AppStorage storage;
  final UsageSource usage;
  final SyncScheduler scheduler;
  final DateTime Function()? now;

  @override
  State<SourcesScreen> createState() => _SourcesScreenState();
}

class _SourcesScreenState extends State<SourcesScreen> with WidgetsBindingObserver {
  List<SourceInfo>? _sources;
  Object? _loadError;
  bool _phoneOn = false, _access = false, _llm = true, _busy = false;
  final _city = TextEditingController();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _load();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _city.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Back from the system screen where the access is granted.
    if (state == AppLifecycleState.resumed) _load();
  }

  Future<void> _load() async {
    try {
      final sources = await widget.api.sources();
      final phoneOn = await widget.storage.usageSyncEnabled();
      final access = await widget.usage.hasAccess();
      final llm = await widget.api.categorizeLlm();
      final city = await widget.api.weatherCity();
      if (!mounted) return;
      if (_city.text.isEmpty) _city.text = city;
      setState(() {
        _sources = sources;
        _phoneOn = phoneOn;
        _access = access;
        _llm = llm;
        _loadError = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loadError = e);
    }
  }

  /// Results are shown in a snack bar: the screen is long, and the buttons
  /// that cause them are far from its top.
  void _say(String text, {bool ok = true}) {
    if (!mounted) return;
    final theme = Theme.of(context);
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(
        content: Text(text),
        backgroundColor: ok ? null : theme.colorScheme.errorContainer,
        showCloseIcon: true,
      ));
  }

  Future<void> _guard(Future<void> Function() body) async {
    setState(() => _busy = true);
    try {
      await body();
    } on ApiException catch (e) {
      _say(e.message, ok: false);
    } catch (_) {
      _say('Не получилось. Попробуй ещё раз.', ok: false);
    }
    if (mounted) setState(() => _busy = false);
  }

  bool _isOn(SourceInfo s) => s.source == 'android_usage' ? s.enabled && _phoneOn : s.enabled;

  Future<void> _toggle(SourceInfo s, bool on) => _guard(() async {
        if (s.source == 'android_usage') {
          if (on && !await widget.usage.hasAccess()) {
            _say('Сначала выдай доступ к истории использования (кнопка ниже).', ok: false);
            return;
          }
          await widget.storage.saveUsageSyncEnabled(on);
          await applyBackground(widget.storage, widget.scheduler);
        }
        await widget.api.setSourceEnabled(s.source, on);
        await _load();
      });

  Future<bool> _confirm(String title, String text, String action) async =>
      await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(title),
          content: Text(text),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
            FilledButton(onPressed: () => Navigator.pop(ctx, true), child: Text(action)),
          ],
        ),
      ) ??
      false;

  Future<void> _deleteSource(SourceInfo s) async {
    if (!await _confirm('Удалить данные источника?', 'С сервера удалятся все сводки источника «${s.title}».', 'Удалить')) {
      return;
    }
    await _guard(() async {
      final n = await widget.api.deleteSourceData(s.source);
      _say('Удалено строк: $n.');
      await _load();
    });
  }

  Future<void> _syncPhone() => _guard(() async {
        final r = await UsageSync(api: widget.api, source: widget.usage, now: widget.now).run();
        if (r.skipped != null) {
          _say(r.skipped!, ok: false);
        } else if (r.unknownPercent > 20) {
          _say('Отправлено дней: ${r.sent}. Не опознано ${r.unknownPercent}% времени: это больше 20%.', ok: false);
        } else {
          _say('Отправлено дней: ${r.sent}. Не опознано ${r.unknownPercent}% времени.');
        }
        await _load();
      });

  Future<void> _saveCity() => _guard(() async {
        final city = _city.text.trim();
        if (city.isEmpty) {
          _say('Введи название города.', ok: false);
          return;
        }
        await widget.api.saveWeatherCity(city);
        _say('Город сохранён. Погода обновится в течение 15 минут.');
      });

  Future<void> _setLlm(bool on) => _guard(() async {
        await widget.api.saveCategorizeLlm(on);
        setState(() => _llm = on);
      });

  Future<void> _export() => _guard(() async {
        final text = await widget.api.exportData();
        await Clipboard.setData(ClipboardData(text: text));
        _say('Экспорт скопирован в буфер обмена (${(text.length / 1024).ceil()} КБ).');
      });

  Future<void> _deleteAll() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => const _TypedConfirm(),
    );
    if (ok != true || !mounted) return;
    await _guard(() async {
      await widget.api.deleteAllData();
      _say('Все данные удалены с сервера.');
      await _load();
    });
  }

  String _stamp(String iso) => iso.length >= 16 ? iso.substring(0, 16).replaceFirst('T', ' ') : iso;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final sources = _sources;
    return Scaffold(
      appBar: AppBar(title: const Text('Источники')),
      body: _loadError != null && sources == null
          ? ErrorView(error: _loadError!, onRetry: _load)
          : sources == null
              ? const Center(child: CircularProgressIndicator())
              : ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    const Text('Агент считает время по категориям сам, ничего отмечать не нужно. '
                        'На сервер уходят только дневные суммы: без названий окон, адресов и заголовков.'),
                    const SizedBox(height: 8),
                    for (final s in sources) _sourceTile(s, theme),
                    const Divider(height: 32),
                    Text('Город для погоды', style: theme.textTheme.titleSmall),
                    const SizedBox(height: 8),
                    Row(children: [
                      Expanded(
                        child: TextField(
                          controller: _city,
                          textCapitalization: TextCapitalization.words,
                          decoration: const InputDecoration(labelText: 'Город', border: OutlineInputBorder()),
                          onSubmitted: (_) => _saveCity(),
                        ),
                      ),
                      const SizedBox(width: 8),
                      FilledButton(onPressed: _busy ? null : _saveCity, child: const Text('Сохранить')),
                    ]),
                    const SizedBox(height: 4),
                    Text('Погода нужна, чтобы ставить спорт в сухое светлое время. Название города уходит в Open-Meteo.',
                        style: theme.textTheme.bodySmall),
                    const Divider(height: 32),
                    Text('Доступ телефона', style: theme.textTheme.titleSmall),
                    const SizedBox(height: 6),
                    Text(_access
                        ? 'Доступ к истории использования выдан.'
                        : 'Чтобы считать время в приложениях, выдай доступ вручную, один раз: кнопка ниже → '
                            'найди «Жизнь» → включи «Разрешить доступ». Android не даёт сделать это из приложения.'),
                    const SizedBox(height: 8),
                    Wrap(spacing: 8, children: [
                      if (!_access)
                        FilledButton(
                          onPressed: _busy ? null : widget.usage.openAccessSettings,
                          child: const Text('Открыть настройки доступа'),
                        ),
                      if (_access)
                        OutlinedButton(onPressed: _busy ? null : _syncPhone, child: const Text('Отправить сейчас')),
                    ]),
                    const Divider(height: 32),
                    SwitchListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('Названия приложений — в языковую модель'),
                      subtitle: const Text('Только названия незнакомых приложений, игр и сайтов, без времени и других '
                          'данных. Бесплатные провайдеры могут их сохранять. Выключено: остаются словарь и «неизвестно».'),
                      value: _llm,
                      onChanged: _busy ? null : _setLlm,
                    ),
                    const Divider(height: 32),
                    OutlinedButton.icon(
                      onPressed: _busy ? null : _export,
                      icon: const Icon(Icons.copy),
                      label: const Text('Экспорт всех данных'),
                    ),
                    const SizedBox(height: 8),
                    OutlinedButton.icon(
                      style: OutlinedButton.styleFrom(foregroundColor: theme.colorScheme.error),
                      onPressed: _busy ? null : _deleteAll,
                      icon: const Icon(Icons.delete_forever),
                      label: const Text('Удалить всё'),
                    ),
                  ],
                ),
    );
  }

  Widget _sourceTile(SourceInfo s, ThemeData theme) {
    final sub = <String>[];
    if (s.lastSync != null) {
      sub.add('Последняя синхронизация: ${_stamp(s.lastSync!)}');
    } else {
      sub.add('Данных пока нет');
    }
    return Column(children: [
      SwitchListTile(
        contentPadding: EdgeInsets.zero,
        title: Text(s.title),
        subtitle: Text(sub.join('\n')),
        value: _isOn(s),
        onChanged: _busy ? null : (on) => _toggle(s, on),
      ),
      if (s.lastError != null)
        Padding(
          padding: const EdgeInsets.only(bottom: 4),
          child: Row(children: [
            Icon(Icons.error_outline, size: 18, color: theme.colorScheme.error),
            const SizedBox(width: 6),
            Expanded(child: Text('Ошибка: ${s.lastError}', style: TextStyle(color: theme.colorScheme.error))),
          ]),
        ),
      Align(
        alignment: Alignment.centerLeft,
        child: TextButton(
          onPressed: _busy ? null : () => _deleteSource(s),
          child: const Text('Удалить данные источника'),
        ),
      ),
    ]);
  }
}

/// "Delete everything" asks to type a word, so a stray tap cannot do it.
class _TypedConfirm extends StatefulWidget {
  const _TypedConfirm();

  @override
  State<_TypedConfirm> createState() => _TypedConfirmState();
}

class _TypedConfirmState extends State<_TypedConfirm> {
  final _c = TextEditingController();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Удалить все данные?'),
      content: Column(mainAxisSize: MainAxisSize.min, children: [
        const Text('С сервера удалится всё: чек-ины, здоровье, активность, задачи, советы. '
            'События в Google Календаре останутся. Настройки моделей тоже.'),
        const SizedBox(height: 12),
        TextField(
          controller: _c,
          autofocus: true,
          decoration: const InputDecoration(labelText: 'Напиши «удалить»', border: OutlineInputBorder()),
          onChanged: (_) => setState(() {}),
        ),
      ]),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Отмена')),
        FilledButton(
          onPressed: _c.text.trim().toLowerCase() == 'удалить' ? () => Navigator.pop(context, true) : null,
          child: const Text('Удалить всё'),
        ),
      ],
    );
  }
}
