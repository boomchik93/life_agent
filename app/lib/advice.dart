import 'models.dart';

/// One advice from the server.
class Advice {
  const Advice({
    required this.id,
    required this.at,
    required this.text,
    required this.source,
    required this.reasons,
  });

  final int id;
  final Wall at; // in the user's zone
  final String text;
  final String source; // auto or user
  final List<String> reasons;

  factory Advice.fromJson(Map<String, dynamic> j) => Advice(
    id: (j['id'] as num).toInt(),
    at: Wall.parse(j['at'] as String),
    text: j['text'] as String,
    source: j['source'] as String? ?? 'auto',
    reasons: [
      for (final r in ((j['reason'] as String?) ?? '').split(','))
        if (r.isNotEmpty) r,
    ],
  );

  bool get asked => source == 'user';

  /// Why an automatic advice came, in words.
  String get reasonText {
    const words = {
      'overload': 'нагрузка выше нормы',
      'sleep': 'недосып',
      'state': 'плохое состояние',
      'events': 'много событий',
    };
    final parts = [
      for (final r in reasons)
        if (words[r] != null) words[r]!,
    ];
    return parts.isEmpty ? '' : parts.join(', ');
  }
}

/// The advice settings the app edits. The server keeps the rest of them.
class AdviceSettings {
  const AdviceSettings({this.includePrivate = false});

  /// Sleep and check-in numbers go to the model. Off by default.
  final bool includePrivate;
}

final _askRe = RegExp(
  r'^(?:дай|дайте|давай|хочу|нужен|нужно|покажи|попроси|попросить)?\s*(?:мне\s+)?(?:новый\s+)?совет\S*(?:\s+по\s+выгоранию)?[\s.!?]*$',
);

/// Whether the typed phrase asks for an advice instead of describing an event.
bool isAdviceRequest(String text) => _askRe.hasMatch(text.trim().toLowerCase());
