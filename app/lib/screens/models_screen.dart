import 'package:flutter/material.dart';

import '../api.dart';
import '../llm_info.dart';
import '../widgets/error_view.dart';

const _resultNames = {
  'ok': 'ок',
  'invalid_json': 'неверный JSON',
  'check_failed': 'не прошёл проверку',
  'timeout': 'таймаут',
  'limit': 'лимит',
  'error': 'ошибка',
};

const _sourceNames = {'user': 'ты', 'auto': 'авто', 'eval': 'проверка'};

/// Model, effort and fallback chain for each LLM task, plus the free quota.
/// The server applies a saved change at once, no restart.
class ModelsScreen extends StatefulWidget {
  const ModelsScreen({super.key, required this.api});

  final LifeApi api;

  @override
  State<ModelsScreen> createState() => _ModelsScreenState();
}

class _ModelsScreenState extends State<ModelsScreen> {
  late Future<LlmInfo> _future = _load();
  LlmInfo? _info;
  Map<String, TaskModels> _saved = {};
  Map<String, TaskModels> _edited = {};
  bool _saving = false;
  String? _saveError;

  Future<LlmInfo> _load() async {
    final info = await widget.api.llmInfo();
    _info = info;
    _saved = Map.of(info.tasks);
    _edited = Map.of(info.tasks);
    return info;
  }

  void _reload() => setState(() {
        _saveError = null;
        _future = _load();
      });

  bool get _dirty {
    for (final e in _edited.entries) {
      if (_saved[e.key] != e.value) return true;
    }
    return false;
  }

  void _update(String task, TaskModels Function(TaskModels) change) {
    setState(() {
      _edited[task] = change(_edited[task]!);
      _saveError = null;
    });
  }

  TaskModels _withModel(TaskModels t, String model) =>
      t.copyWith(model: model, reasoning: _info?.model(model)?.reasoning ?? false);

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _saveError = null;
    });
    try {
      await widget.api.saveLlmTasks(_edited);
      // Reload so the effort control follows what the server knows about the models.
      final info = await widget.api.llmInfo();
      if (!mounted) return;
      setState(() {
        _info = info;
        _saved = Map.of(info.tasks);
        _edited = Map.of(info.tasks);
      });
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('Сохранено. Работает сразу, без перезапуска.')));
    } on ApiException catch (e) {
      if (mounted) setState(() => _saveError = e.message);
    }
    if (mounted) setState(() => _saving = false);
  }

  Future<bool> _confirmLeave() async {
    final leave = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Выйти без сохранения?'),
        content: const Text('Изменения моделей не сохранены.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Остаться')),
          TextButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Выйти')),
        ],
      ),
    );
    return leave ?? false;
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: !_dirty,
      onPopInvokedWithResult: (didPop, _) async {
        if (didPop) return;
        final nav = Navigator.of(context);
        if (await _confirmLeave()) nav.pop();
      },
      child: Scaffold(
        appBar: AppBar(title: const Text('Модели')),
        body: FutureBuilder<LlmInfo>(
          future: _future,
          builder: (context, snap) {
            if (snap.connectionState != ConnectionState.done) {
              return const Center(child: CircularProgressIndicator());
            }
            if (snap.hasError) return ErrorView(error: snap.error!, onRetry: _reload);
            final info = _info!;
            return Column(
              children: [
                Expanded(
                  child: ListView(
                    padding: const EdgeInsets.all(16),
                    children: [
                      for (final w in info.warnings) _Note(Icons.info_outline, w),
                      if (info.quota != null) _QuotaCard(quota: info.quota!),
                      for (final e in llmTaskNames.entries)
                        if (_edited[e.key] != null)
                          _TaskCard(
                            title: e.value,
                            mandatory: e.key == 'parse',
                            task: _edited[e.key]!,
                            info: info,
                            onModel: (m) => _update(e.key, (t) => _withModel(t, m)),
                            onEffort: (v) => _update(e.key, (t) => t.copyWith(effort: v)),
                            onFallbacks: (f) => _update(e.key, (t) => t.copyWith(fallbacks: f)),
                          ),
                      if (info.recentCalls.isNotEmpty) _RecentCalls(calls: info.recentCalls),
                    ],
                  ),
                ),
                _SaveBar(
                  dirty: _dirty,
                  saving: _saving,
                  error: _saveError,
                  onSave: _save,
                  onReset: () => setState(() {
                    _edited = Map.of(_saved);
                    _saveError = null;
                  }),
                ),
              ],
            );
          },
        ),
      ),
    );
  }
}

class _SaveBar extends StatelessWidget {
  const _SaveBar({
    required this.dirty,
    required this.saving,
    required this.error,
    required this.onSave,
    required this.onReset,
  });

  final bool dirty, saving;
  final String? error;
  final VoidCallback onSave, onReset;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      elevation: 6,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Row(children: [
                    Icon(Icons.error, color: theme.colorScheme.error),
                    const SizedBox(width: 8),
                    Expanded(child: Text(error!)),
                  ]),
                ),
              Row(children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: dirty && !saving ? onReset : null,
                    child: const Text('Сбросить'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: FilledButton(
                    onPressed: dirty && !saving ? onSave : null,
                    child: saving
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Text('Сохранить'),
                  ),
                ),
              ]),
            ],
          ),
        ),
      ),
    );
  }
}

class _Note extends StatelessWidget {
  const _Note(this.icon, this.text);

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: Theme.of(context).colorScheme.tertiaryContainer,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [Icon(icon), const SizedBox(width: 10), Expanded(child: Text(text))],
        ),
      ),
    );
  }
}

class _QuotaCard extends StatelessWidget {
  const _QuotaCard({required this.quota});

  final Quota quota;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final low = quota.remaining < 10;
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Запросы к бесплатным моделям', style: theme.textTheme.titleSmall),
            const SizedBox(height: 8),
            LinearProgressIndicator(
              value: quota.limit == 0 ? 0 : (quota.used / quota.limit).clamp(0, 1).toDouble(),
              minHeight: 8,
              borderRadius: BorderRadius.circular(4),
              color: low ? theme.colorScheme.error : null,
            ),
            const SizedBox(height: 8),
            Text('Осталось ${quota.remaining} из ${quota.limit} за сутки. Сброс в ${quota.resetAt.hm}.',
                style: low ? TextStyle(color: theme.colorScheme.error) : null),
            if (low)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text('Автосовет не запускается, пока остаток меньше 10: квота нужна для разбора фраз.',
                    style: theme.textTheme.bodySmall),
              ),
          ],
        ),
      ),
    );
  }
}

class _TaskCard extends StatelessWidget {
  const _TaskCard({
    required this.title,
    required this.mandatory,
    required this.task,
    required this.info,
    required this.onModel,
    required this.onEffort,
    required this.onFallbacks,
  });

  final String title;
  final bool mandatory; // the task does not work without a model
  final TaskModels task;
  final LlmInfo info;
  final ValueChanged<String> onModel, onEffort;
  final ValueChanged<List<String>> onFallbacks;

  String _label(String id) => info.model(id)?.name ?? id;

  Future<String?> _pick(BuildContext context, {required String title}) {
    final taken = {task.model, ...task.fallbacks};
    return showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (_) => _ModelPicker(title: title, models: info.models, taken: taken),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final noModel = task.model.isEmpty;
    final canAdd = task.fallbacks.length < info.maxFallback;
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Text(title, style: theme.textTheme.titleMedium),
            ),
            ListTile(
              title: const Text('Основная модель'),
              subtitle: Text(
                noModel ? (mandatory ? 'Не выбрана — без неё фразы не разбираются' : 'Не выбрана') : _label(task.model),
                style: noModel && mandatory ? TextStyle(color: theme.colorScheme.error) : null,
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: () async {
                final m = await _pick(context, title: 'Основная модель');
                if (m != null) onModel(m);
              },
            ),
            if (task.reasoning)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Эффорт (глубина рассуждений)', style: theme.textTheme.bodySmall),
                    const SizedBox(height: 6),
                    SizedBox(
                      width: double.infinity,
                      child: SegmentedButton<String>(
                        showSelectedIcon: false,
                        segments: [
                          for (final e in efforts) ButtonSegment(value: e, label: Text(effortNames[e]!)),
                        ],
                        selected: {efforts.contains(task.effort) ? task.effort : 'low'},
                        onSelectionChanged: (s) => onEffort(s.first),
                      ),
                    ),
                  ],
                ),
              )
            else if (!noModel)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                child: Text('У этой модели нет режима рассуждений, эффорт не нужен.',
                    style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor)),
              ),
            const Divider(),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
              child: Text('Резервные модели (${task.fallbacks.length} из ${info.maxFallback})',
                  style: theme.textTheme.titleSmall),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 2, 16, 4),
              child: Text('Если основная модель не ответила, запрос уходит на следующую по порядку.',
                  style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor)),
            ),
            for (var i = 0; i < task.fallbacks.length; i++)
              ListTile(
                dense: true,
                leading: CircleAvatar(radius: 14, child: Text('${i + 1}')),
                title: Text(_label(task.fallbacks[i]), maxLines: 2, overflow: TextOverflow.ellipsis),
                trailing: Row(mainAxisSize: MainAxisSize.min, children: [
                  IconButton(
                    tooltip: 'Выше',
                    icon: const Icon(Icons.arrow_upward),
                    onPressed: i == 0 ? null : () => onFallbacks(_swap(task.fallbacks, i, i - 1)),
                  ),
                  IconButton(
                    tooltip: 'Ниже',
                    icon: const Icon(Icons.arrow_downward),
                    onPressed: i == task.fallbacks.length - 1
                        ? null
                        : () => onFallbacks(_swap(task.fallbacks, i, i + 1)),
                  ),
                  IconButton(
                    tooltip: 'Убрать',
                    icon: const Icon(Icons.close),
                    onPressed: () => onFallbacks([...task.fallbacks]..removeAt(i)),
                  ),
                ]),
              ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: TextButton.icon(
                icon: const Icon(Icons.add),
                label: Text(canAdd ? 'Добавить резервную' : 'Достигнут предел цепочки'),
                onPressed: canAdd
                    ? () async {
                        final m = await _pick(context, title: 'Резервная модель');
                        if (m != null) onFallbacks([...task.fallbacks, m]);
                      }
                    : null,
              ),
            ),
          ],
        ),
      ),
    );
  }

  static List<String> _swap(List<String> l, int a, int b) {
    final out = [...l];
    final t = out[a];
    out[a] = out[b];
    out[b] = t;
    return out;
  }
}

/// A searchable list of the free models, with a way to type an id by hand.
class _ModelPicker extends StatefulWidget {
  const _ModelPicker({required this.title, required this.models, required this.taken});

  final String title;
  final List<ModelInfo> models;
  final Set<String> taken; // already in this task's chain

  @override
  State<_ModelPicker> createState() => _ModelPickerState();
}

class _ModelPickerState extends State<_ModelPicker> {
  String _query = '';

  bool _validId(String s) => s.contains('/') && !RegExp(r'\s').hasMatch(s) && s.length <= 120;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final q = _query.trim().toLowerCase();
    final shown = [
      for (final m in widget.models)
        if (!widget.taken.contains(m.id) && (q.isEmpty || m.id.toLowerCase().contains(q) || m.name.toLowerCase().contains(q)))
          m,
    ];
    final typed = _query.trim();
    final manual = _validId(typed) && !widget.taken.contains(typed) && !shown.any((m) => m.id == typed);
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.of(context).viewInsets.bottom),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(widget.title, style: theme.textTheme.titleMedium),
                const SizedBox(height: 8),
                TextField(
                  autofocus: true,
                  autocorrect: false,
                  decoration: const InputDecoration(
                    prefixIcon: Icon(Icons.search),
                    hintText: 'Поиск или id вида провайдер/модель:free',
                    border: OutlineInputBorder(),
                  ),
                  onChanged: (v) => setState(() => _query = v),
                ),
              ],
            ),
          ),
          Expanded(
            child: ListView(
              children: [
                if (manual)
                  ListTile(
                    leading: const Icon(Icons.edit),
                    title: Text('Использовать «$typed»'),
                    subtitle: const Text('Модели нет в списке, id введён вручную'),
                    onTap: () => Navigator.pop(context, typed),
                  ),
                for (final m in shown)
                  ListTile(
                    title: Text(m.name, maxLines: 2, overflow: TextOverflow.ellipsis),
                    subtitle: Text(m.id, maxLines: 1, overflow: TextOverflow.ellipsis),
                    trailing: m.reasoning
                        ? Tooltip(message: 'Умеет рассуждать, есть эффорт', child: const Icon(Icons.psychology))
                        : null,
                    onTap: () => Navigator.pop(context, m.id),
                  ),
                if (shown.isEmpty && !manual)
                  Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text(
                      widget.models.isEmpty
                          ? 'Список моделей не загрузился. Введи id вручную, например provider/model:free.'
                          : 'Ничего не найдено. Id можно ввести вручную: provider/model:free.',
                      textAlign: TextAlign.center,
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _RecentCalls extends StatelessWidget {
  const _RecentCalls({required this.calls});

  final List<LlmCall> calls;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: ExpansionTile(
        shape: const Border(),
        title: const Text('Последние вызовы'),
        subtitle: const Text('Видно, если модель стала подводить'),
        children: [
          for (final c in calls)
            ListTile(
              dense: true,
              title: Text(c.model, maxLines: 1, overflow: TextOverflow.ellipsis),
              subtitle: Text(
                '${c.time.hm} · ${llmTaskNames[c.task] ?? c.task} · ${_sourceNames[c.source] ?? c.source}'
                '${c.chainPos > 0 ? ' · резервная ${c.chainPos}' : ''}'
                '${c.attempt > 1 ? ' · повтор' : ''}',
              ),
              trailing: Text(
                _resultNames[c.result] ?? c.result,
                style: TextStyle(
                  color: c.result == 'ok' ? null : theme.colorScheme.error,
                  fontWeight: c.result == 'ok' ? null : FontWeight.bold,
                ),
              ),
            ),
        ],
      ),
    );
  }
}
