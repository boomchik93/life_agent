import 'package:flutter/material.dart';

import '../api.dart';

/// Parses hours typed with a comma or a dot; empty means "not entered".
/// Returns null for an empty string, throws FormatException for garbage.
double? parseSleepHours(String raw) {
  final s = raw.trim().replaceAll(',', '.');
  if (s.isEmpty) return null;
  final v = double.tryParse(s);
  if (v == null || v < 0 || v > 24) throw const FormatException('sleep');
  return v;
}

/// Daily check-in: two taps and an optional number. Pops `true` once saved.
/// On an error the entered values stay on screen.
class CheckinScreen extends StatefulWidget {
  const CheckinScreen({super.key, required this.api});

  final LifeApi api;

  @override
  State<CheckinScreen> createState() => _CheckinScreenState();
}

class _CheckinScreenState extends State<CheckinScreen> {
  int? _energy;
  int? _mood;
  final _sleep = TextEditingController();
  String? _sleepError;
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _sleep.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    double? sleep;
    try {
      sleep = parseSleepHours(_sleep.text);
      _sleepError = null;
    } on FormatException {
      setState(() => _sleepError = 'Часы сна: число от 0 до 24');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.api.checkin(energy: _energy!, mood: _mood!, sleepHours: sleep);
      if (mounted) Navigator.of(context).pop(true);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final ready = _energy != null && _mood != null && !_busy;
    return Scaffold(
      appBar: AppBar(title: const Text('Чек-ин')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _Scale(
            title: 'Энергия',
            low: 'нет сил',
            high: 'бодрый',
            value: _energy,
            onChanged: (v) => setState(() => _energy = v),
          ),
          const SizedBox(height: 24),
          _Scale(
            title: 'Настроение',
            low: 'плохое',
            high: 'отличное',
            value: _mood,
            onChanged: (v) => setState(() => _mood = v),
          ),
          const SizedBox(height: 24),
          TextField(
            controller: _sleep,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(
              labelText: 'Сколько часов спал (необязательно)',
              hintText: '7,5',
              border: const OutlineInputBorder(),
              errorText: _sleepError,
            ),
          ),
          const SizedBox(height: 24),
          FilledButton(
            onPressed: ready ? _save : null,
            child: _busy
                ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                : const Text('Сохранить'),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 16),
              child: Row(children: [
                Icon(Icons.error, color: Theme.of(context).colorScheme.error),
                const SizedBox(width: 8),
                Expanded(child: Text('$_error Ответы сохранены на экране, нажми «Сохранить» ещё раз.')),
              ]),
            ),
        ],
      ),
    );
  }
}

class _Scale extends StatelessWidget {
  const _Scale({
    required this.title,
    required this.low,
    required this.high,
    required this.value,
    required this.onChanged,
  });

  final String title, low, high;
  final int? value;
  final ValueChanged<int> onChanged;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        Row(
          children: [
            for (var i = 1; i <= 5; i++)
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 3),
                  child: Semantics(
                    label: '$title $i из 5',
                    selected: value == i,
                    child: ChoiceChip(
                      label: SizedBox(width: double.infinity, child: Text('$i', textAlign: TextAlign.center)),
                      showCheckmark: false,
                      selected: value == i,
                      onSelected: (_) => onChanged(i),
                      padding: const EdgeInsets.symmetric(vertical: 10),
                    ),
                  ),
                ),
              ),
          ],
        ),
        const SizedBox(height: 4),
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text('1 — $low', style: Theme.of(context).textTheme.bodySmall),
            Text('5 — $high', style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
      ],
    );
  }
}
