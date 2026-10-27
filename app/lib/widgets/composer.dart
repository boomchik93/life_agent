import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../advice.dart';
import '../api.dart';
import '../screens/card_screen.dart';

const maxPhraseLength = 500;

/// The input line of the home screen. Voice input is the microphone of the
/// keyboard. The typed text is never cleared by an error.
class Composer extends StatefulWidget {
  const Composer({super.key, required this.api, this.now, this.onOpenModels, this.onSaved, this.onAdvice});

  final LifeApi api;
  final DateTime Function()? now;

  /// Called after an event is written or a task is saved, to refresh the week.
  final VoidCallback? onSaved;

  /// Called when the phrase asks for an advice ("дай совет") instead of
  /// describing an event or a task.
  final VoidCallback? onAdvice;

  /// Opens the model settings; offered when the models are the problem.
  final VoidCallback? onOpenModels;

  @override
  State<Composer> createState() => _ComposerState();
}

class _ComposerState extends State<Composer> {
  final _text = TextEditingController();
  final _focus = FocusNode();
  bool _busy = false;
  ApiException? _error;
  int _ticket = 0; // a newer request or a cancel makes older answers stale

  @override
  void initState() {
    super.initState();
    _text.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _text.dispose();
    _focus.dispose();
    super.dispose();
  }

  bool get _canSend => !_busy && _text.text.trim().isNotEmpty;

  Future<void> _send() async {
    if (!_canSend) return;
    if (widget.onAdvice != null && isAdviceRequest(_text.text)) {
      _text.clear();
      _focus.unfocus();
      widget.onAdvice!();
      return;
    }
    final ticket = ++_ticket;
    _focus.unfocus();
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final card = await widget.api.parse(_text.text.trim());
      if (!mounted || ticket != _ticket) return;
      setState(() => _busy = false);
      final done = await Navigator.of(context).push<String>(
        MaterialPageRoute(builder: (_) => CardScreen(api: widget.api, card: card, now: widget.now)),
      );
      if (done != null && mounted) {
        // Written: the phrase has served its purpose.
        _text.clear();
        widget.onSaved?.call();
        ScaffoldMessenger.of(context)
          ..hideCurrentSnackBar()
          ..showSnackBar(SnackBar(content: Text(done)));
      }
    } on ApiException catch (e) {
      if (!mounted || ticket != _ticket) return;
      setState(() {
        _busy = false;
        _error = e;
      });
    }
  }

  void _cancel() {
    _ticket++;
    setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final error = _error;
    return Material(
      elevation: 8,
      color: theme.colorScheme.surface,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (error != null) _ErrorLine(error: error, onModels: widget.onOpenModels),
              if (_busy)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Row(children: [
                    const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)),
                    const SizedBox(width: 12),
                    const Expanded(child: Text('Разбираю фразу… это может занять до минуты')),
                    TextButton(onPressed: _cancel, child: const Text('Отмена')),
                  ]),
                ),
              Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Expanded(
                    child: TextField(
                      controller: _text,
                      focusNode: _focus,
                      readOnly: _busy,
                      minLines: 1,
                      maxLines: 4,
                      keyboardType: TextInputType.multiline,
                      textInputAction: TextInputAction.send,
                      textCapitalization: TextCapitalization.sentences,
                      inputFormatters: [LengthLimitingTextInputFormatter(maxPhraseLength)],
                      onSubmitted: (_) => _send(),
                      decoration: InputDecoration(
                        hintText: 'КВН в пятницу с 18 до 20',
                        helperText: 'Событие или задача с дедлайном. Можно надиктовать.',
                        border: const OutlineInputBorder(borderRadius: BorderRadius.all(Radius.circular(24))),
                        contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                        suffixIcon: _text.text.isEmpty || _busy
                            ? null
                            : IconButton(
                                tooltip: 'Очистить',
                                icon: const Icon(Icons.close),
                                onPressed: () {
                                  _text.clear();
                                  setState(() => _error = null);
                                },
                              ),
                      ),
                    ),
                  ),
                  Padding(
                    padding: const EdgeInsets.only(left: 4, bottom: 22),
                    child: IconButton.filled(
                      tooltip: 'Разобрать',
                      iconSize: 28,
                      constraints: const BoxConstraints(minWidth: 52, minHeight: 52),
                      icon: const Icon(Icons.arrow_upward),
                      onPressed: _canSend ? _send : null,
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ErrorLine extends StatelessWidget {
  const _ErrorLine({required this.error, required this.onModels});

  final ApiException error;
  final VoidCallback? onModels;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.only(bottom: 8, right: 4),
      color: theme.colorScheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  error.problem == ApiProblem.dailyLimit ? Icons.hourglass_bottom : Icons.error_outline,
                  color: theme.colorScheme.onErrorContainer,
                ),
                const SizedBox(width: 10),
                Expanded(child: Text('${error.message} Текст сохранён, можно нажать отправить ещё раз.')),
              ],
            ),
            if (onModels != null && error.problem == ApiProblem.llm)
              Align(
                alignment: Alignment.centerRight,
                child: TextButton(onPressed: onModels, child: const Text('Модели')),
              ),
          ],
        ),
      ),
    );
  }
}
