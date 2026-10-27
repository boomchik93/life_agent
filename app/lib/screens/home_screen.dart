import 'package:flutter/material.dart';

import '../controller.dart';
import '../format.dart';
import '../models.dart';
import '../widgets/error_view.dart';
import '../widgets/composer.dart';
import '../widgets/risk_indicator.dart';
import '../widgets/week_view.dart';
import 'checkin_screen.dart';
import 'models_screen.dart';
import 'settings_screen.dart';
import 'week_screen.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.controller, this.now});

  final AppController controller;
  final DateTime Function()? now;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Future<WeekData>? _future;
  bool _checkinDone = true; // hidden until the stored date is read

  AppController get _c => widget.controller;
  Wall get _today => wallNow(widget.now?.call());

  @override
  void initState() {
    super.initState();
    _c.addListener(_reload);
    _future = _c.api?.week();
    _readCheckinDate();
  }

  @override
  void dispose() {
    _c.removeListener(_reload);
    super.dispose();
  }

  Future<void> _readCheckinDate() async {
    final last = await _c.storage.lastCheckinDate();
    if (mounted) setState(() => _checkinDone = last == _today.ymd);
  }

  void _reload() {
    final future = _c.api?.week();
    setState(() {
      _future = future;
    });
  }

  Future<void> _openCheckin() async {
    final api = _c.api;
    if (api == null) return;
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => CheckinScreen(api: api)),
    );
    if (saved == true) {
      await _c.storage.saveCheckinDate(_today.ymd);
      if (!mounted) return;
      setState(() => _checkinDone = true);
      _reload();
    }
  }

  Future<void> _openSettings() async {
    await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => SettingsScreen(controller: _c)),
    );
    // The controller notifies on save, which reloads the week.
  }

  void _openModels() {
    final api = _c.api;
    if (api == null) return;
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => ModelsScreen(api: api)));
  }

  void _openWeek() {
    final api = _c.api;
    if (api == null) return;
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => WeekScreen(api: api, now: widget.now)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Жизнь'),
        actions: [
          IconButton(tooltip: 'Неделя', icon: const Icon(Icons.calendar_month), onPressed: _openWeek),
          IconButton(tooltip: 'Настройки', icon: const Icon(Icons.settings), onPressed: _openSettings),
        ],
      ),
      body: Column(
        children: [
          Expanded(child: _body()),
          if (_c.api != null) Composer(api: _c.api!, now: widget.now, onOpenModels: _openModels, onSaved: _reload),
        ],
      ),
    );
  }

  Widget _body() {
    final future = _future;
    if (future == null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('Укажи адрес сервера и токен.', textAlign: TextAlign.center),
              const SizedBox(height: 16),
              FilledButton(onPressed: _openSettings, child: const Text('Настройки')),
            ],
          ),
        ),
      );
    }
    return FutureBuilder<WeekData>(
      future: future,
      builder: (context, snap) {
        if (snap.connectionState != ConnectionState.done) {
          return const Center(child: CircularProgressIndicator());
        }
        if (snap.hasError) {
          return ErrorView(error: snap.error!, onRetry: _reload, onSettings: _openSettings);
        }
        final week = snap.data!;
        return RefreshIndicator(
          onRefresh: () async {
            _reload();
            await _future!.catchError((_) => week);
          },
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (!_checkinDone) _CheckinCard(onTap: _openCheckin),
              if (week.warnings.isNotEmpty) _Warnings(week.warnings),
              RiskGrid(risks: week.risks),
              WeekView(week: week, today: _today),
            ],
          ),
        );
      },
    );
  }
}

class _CheckinCard extends StatelessWidget {
  const _CheckinCard({required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: Theme.of(context).colorScheme.primaryContainer,
      child: ListTile(
        leading: const Icon(Icons.favorite),
        title: const Text('Как ты сегодня?'),
        subtitle: const Text('Чек-ин займёт 5 секунд'),
        trailing: const Icon(Icons.chevron_right),
        onTap: onTap,
      ),
    );
  }
}

class _Warnings extends StatelessWidget {
  const _Warnings(this.warnings);

  final List<String> warnings;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: Theme.of(context).colorScheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Icon(Icons.sync_problem),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                'Часть календарей не прочиталась, оценка неполная:\n${warnings.join('\n')}',
              ),
            ),
          ],
        ),
      ),
    );
  }
}
