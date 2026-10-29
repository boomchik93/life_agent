import 'dart:convert';

import 'package:flutter/material.dart';

import '../api.dart';
import '../card.dart';
import '../format.dart';
import '../models.dart';
import '../slot.dart';
import 'verdict_screen.dart';

/// The result of a parse as a card the user can fix. What the model filled in
/// by itself is named so the user can check it; fields still empty are marked. An event goes through the server's check before it
/// is written; a task is saved as it is. Pops the text to show once saved.
class CardScreen extends StatefulWidget {
  const CardScreen({super.key, required this.api, required this.card, this.now});

  final LifeApi api;
  final ParsedCard card;
  final DateTime Function()? now;

  @override
  State<CardScreen> createState() => _CardScreenState();
}

class _CardScreenState extends State<CardScreen> {
  late final CardDraft _d = CardDraft.fromParsed(widget.card);
  late final _title = TextEditingController(text: _d.title);

  final _keys = IdempotencyKeys();
  bool _saving = false;
  String? _saveError;

  Wall get _now => wallNow(widget.now?.call());

  bool get _ready => _d.isComplete && !_saving;

  Future<void> _primary() async {
    if (_d.isEvent) {
      final written = await Navigator.of(context).push<bool>(
        MaterialPageRoute(builder: (_) => VerdictScreen(api: widget.api, slot: _d.toSlot())),
      );
      if (written == true && mounted) Navigator.of(context).pop('Записано в календарь');
      return;
    }
    final task = _d.toTask();
    setState(() {
      _saving = true;
      _saveError = null;
    });
    try {
      await widget.api.saveTask(task, key: _keys.forPayload(jsonEncode(task.toJson())));
      if (mounted) Navigator.of(context).pop('Задача сохранена');
      return;
    } on ApiException catch (e) {
      if (mounted) setState(() => _saveError = e.message);
    }
    if (mounted) setState(() => _saving = false);
  }

  @override
  void dispose() {
    _title.dispose();
    super.dispose();
  }

  void _edit(VoidCallback change) => setState(change);

  void _setKind(String kind) {
    if (kind == _d.kind) return;
    _edit(() {
      if (kind == 'task') {
        _d.durationMin ??= _d.eventMinutes;
        _d.deadline ??= _d.date;
      } else {
        _d.date ??= _d.deadline;
      }
      _d.kind = kind;
    });
  }

  Future<Wall?> _pickDay(Wall? current) async {
    final today = _now.dayStart;
    final initial = current == null || current.isBefore(today) ? today : current;
    final picked = await showDatePicker(
      context: context,
      initialDate: DateTime(initial.year, initial.month, initial.day),
      firstDate: DateTime(today.year, today.month, today.day),
      lastDate: DateTime(today.year + 3, today.month, today.day),
    );
    return picked == null ? null : Wall(picked.year, picked.month, picked.day);
  }

  Future<int?> _pickTime(int? current, int fallback) async {
    final at = current ?? fallback;
    final picked = await showTimePicker(
      context: context,
      initialTime: TimeOfDay(hour: at ~/ 60, minute: at % 60),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(alwaysUse24HourFormat: true),
        child: child!,
      ),
    );
    return picked == null ? null : picked.hour * 60 + picked.minute;
  }

  Future<void> _pickDate() async {
    final d = await _pickDay(_d.date);
    if (d != null) _edit(() => _d.date = d);
  }

  Future<void> _pickDeadline() async {
    final d = await _pickDay(_d.deadline);
    if (d != null) _edit(() => _d.deadline = d);
  }

  Future<void> _pickFrom() async {
    final t = await _pickTime(_d.from, 9 * 60);
    if (t == null) return;
    _edit(() {
      _d.from = t;
      // Keep an hour for the event unless the user already set an end after the start.
      if (_d.to == null || _d.to! <= t) _d.to = t + 60 < 24 * 60 ? t + 60 : null;
    });
  }

  Future<void> _pickTo() async {
    final t = await _pickTime(_d.to, (_d.from ?? 9 * 60) + 60);
    if (t != null) _edit(() => _d.to = t);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final missing = _d.missing();
    final guessed = widget.card.guessed.where((g) => !missing.contains(g)).toList();
    final title = _d.title.trim().isEmpty;
    final problems = _d.problems(_now);
    return Scaffold(
      appBar: AppBar(title: Text(_d.isEvent ? 'Событие' : 'Задача')),
      body: Column(
        children: [
          Expanded(
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                SegmentedButton<String>(
                  segments: const [
                    ButtonSegment(value: 'event', icon: Icon(Icons.event), label: Text('Событие')),
                    ButtonSegment(value: 'task', icon: Icon(Icons.task_alt), label: Text('Задача')),
                  ],
                  selected: {_d.kind},
                  onSelectionChanged: (s) => _setKind(s.first),
                ),
                const SizedBox(height: 16),
                if (missing.isNotEmpty)
                  _Banner(
                    icon: Icons.edit_note,
                    text: 'Не хватает: ${fieldsText(missing)}. Заполни ниже.',
                    color: theme.colorScheme.tertiaryContainer,
                  ),
                if (guessed.isNotEmpty)
                  _Banner(
                    icon: Icons.auto_awesome,
                    text: 'Модель додумала: ${fieldsText(guessed)}. Поправь, если не так.',
                    color: theme.colorScheme.secondaryContainer,
                  ),
                for (final p in problems)
                  _Banner(icon: Icons.warning_amber, text: p, color: theme.colorScheme.errorContainer),
                TextField(
                  controller: _title,
                  textCapitalization: TextCapitalization.sentences,
                  decoration: InputDecoration(
                    labelText: 'Название',
                    border: const OutlineInputBorder(),
                    errorText: title ? 'Впиши название' : null,
                  ),
                  onChanged: (v) => _edit(() => _d.title = v),
                ),
                const SizedBox(height: 16),
                if (_d.isEvent) ..._eventFields(missing) else ..._taskFields(missing),
                const SizedBox(height: 8),
                Text('Категория', style: theme.textTheme.titleSmall),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 4,
                  children: [
                    for (final e in categoryNames.entries)
                      ChoiceChip(
                        label: Text(e.value),
                        selected: _d.category == e.key,
                        onSelected: (_) => _edit(() => _d.category = e.key),
                      ),
                  ],
                ),
                if (widget.card.model.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  Text('Разобрано моделью ${widget.card.model}',
                      style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor)),
                ],
              ],
            ),
          ),
          _BottomBar(
            isEvent: _d.isEvent,
            ready: _ready,
            saving: _saving,
            error: _saveError,
            hint: _d.title.trim().isEmpty || _d.missing().isNotEmpty
                ? 'Заполни недостающие поля.'
                : _d.isEvent && _d.to! <= _d.from!
                    ? 'Конец должен быть позже начала.'
                    : null,
            onPrimary: _primary,
          ),
        ],
      ),
    );
  }

  List<Widget> _eventFields(List<String> missing) {
    final d = _d.date;
    final today = _now.dayStart;
    return [
      Wrap(spacing: 8, children: [
        ChoiceChip(
          label: const Text('Сегодня'),
          selected: d == today,
          onSelected: (_) => _edit(() => _d.date = today),
        ),
        ChoiceChip(
          label: const Text('Завтра'),
          selected: d == today.addDays(1),
          onSelected: (_) => _edit(() => _d.date = today.addDays(1)),
        ),
      ]),
      const SizedBox(height: 8),
      _PickerField(
        label: 'Дата',
        value: d == null ? null : dayTitle(d),
        icon: Icons.calendar_today,
        missing: missing.contains('date'),
        onTap: _pickDate,
      ),
      const SizedBox(height: 12),
      Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: _PickerField(
              label: 'Начало',
              value: _d.from == null ? null : minutesHm(_d.from!),
              icon: Icons.schedule,
              missing: missing.contains('time_from'),
              onTap: _pickFrom,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: _PickerField(
              label: 'Конец',
              value: _d.to == null ? null : minutesHm(_d.to!),
              icon: Icons.schedule,
              missing: missing.contains('time_to'),
              onTap: _pickTo,
            ),
          ),
        ],
      ),
      if (_d.eventMinutes != null)
        Padding(
          padding: const EdgeInsets.only(top: 6, left: 4),
          child: Text('Длится ${durationText(_d.eventMinutes!)}'),
        ),
      if (_d.recurrence != null)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Row(children: [
            const Icon(Icons.repeat, size: 20),
            const SizedBox(width: 8),
            Expanded(child: Text('Повторяется ${recurrenceText(_d.recurrence!)}')),
          ]),
        ),
      const SizedBox(height: 16),
    ];
  }

  List<Widget> _taskFields(List<String> missing) {
    final theme = Theme.of(context);
    final dur = _d.durationMin;
    final today = _now.dayStart;
    final durMissing = missing.contains('duration_min');
    return [
      Text('Сколько времени нужно', style: theme.textTheme.titleSmall),
      const SizedBox(height: 8),
      Row(
        children: [
          IconButton.filledTonal(
            tooltip: 'Меньше на 30 минут',
            icon: const Icon(Icons.remove),
            onPressed: dur == null || dur <= 30 ? null : () => _edit(() => _d.durationMin = dur - 30),
          ),
          Expanded(
            child: Text(
              dur == null ? 'Не указано' : durationText(dur),
              textAlign: TextAlign.center,
              style: theme.textTheme.headlineSmall?.copyWith(
                color: durMissing ? theme.colorScheme.error : null,
              ),
            ),
          ),
          IconButton.filledTonal(
            tooltip: 'Больше на 30 минут',
            icon: const Icon(Icons.add),
            onPressed: dur != null && dur >= 24 * 60
                ? null
                : () => _edit(() => _d.durationMin = (dur ?? 30) + 30),
          ),
        ],
      ),
      const SizedBox(height: 8),
      Wrap(spacing: 8, children: [
        for (final m in const [30, 60, 120, 180, 360])
          ChoiceChip(
            label: Text(durationText(m)),
            selected: dur == m,
            onSelected: (_) => _edit(() => _d.durationMin = m),
          ),
      ]),
      const SizedBox(height: 20),
      Text('Срок', style: theme.textTheme.titleSmall),
      const SizedBox(height: 8),
      Wrap(spacing: 8, children: [
        ChoiceChip(
          label: const Text('Сегодня'),
          selected: _d.deadline == today,
          onSelected: (_) => _edit(() => _d.deadline = today),
        ),
        ChoiceChip(
          label: const Text('Завтра'),
          selected: _d.deadline == today.addDays(1),
          onSelected: (_) => _edit(() => _d.deadline = today.addDays(1)),
        ),
      ]),
      const SizedBox(height: 8),
      _PickerField(
        label: 'Дедлайн',
        value: _d.deadline == null ? null : dayTitle(_d.deadline!),
        icon: Icons.flag,
        missing: missing.contains('deadline'),
        onTap: _pickDeadline,
      ),
      const SizedBox(height: 16),
    ];
  }
}

class _BottomBar extends StatelessWidget {
  const _BottomBar({
    required this.isEvent,
    required this.ready,
    required this.saving,
    required this.error,
    required this.hint,
    required this.onPrimary,
  });

  final bool isEvent, ready, saving;
  final String? error, hint;
  final VoidCallback onPrimary;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      elevation: 6,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
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
              if (!ready && hint != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(hint!, style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor)),
                ),
              Row(children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: saving ? null : () => Navigator.of(context).pop(),
                    child: const Text('Назад к фразе'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  flex: 2,
                  child: FilledButton(
                    onPressed: ready ? onPrimary : null,
                    child: saving
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        : Text(isEvent ? 'Проверить и записать' : 'Сохранить задачу'),
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

class _Banner extends StatelessWidget {
  const _Banner({required this.icon, required this.text, required this.color});

  final IconData icon;
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: color,
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

/// A tappable field that opens a picker. An empty required one is marked red.
class _PickerField extends StatelessWidget {
  const _PickerField({
    required this.label,
    required this.value,
    required this.icon,
    required this.missing,
    required this.onTap,
  });

  final String label;
  final String? value;
  final IconData icon;
  final bool missing;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      button: true,
      label: '$label: ${value ?? 'не указано'}',
      excludeSemantics: true,
      onTap: onTap,
      child: InkWell(
        borderRadius: BorderRadius.circular(4),
        onTap: onTap,
        child: InputDecorator(
          isEmpty: value == null,
          decoration: InputDecoration(
            labelText: label,
            prefixIcon: Icon(icon),
            border: const OutlineInputBorder(),
            errorText: missing ? 'Не указано' : null,
          ),
          child: Text(value ?? ''),
        ),
      ),
    );
  }
}
