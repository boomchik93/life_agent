import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../advice.dart';
import '../api.dart';
import '../card.dart';
import '../format.dart';
import '../platform.dart';
import '../screens/card_screen.dart';
import '../slot.dart';
import '../theme.dart';

const maxPhraseLength = 500;

/// The input line of the home screen. Voice input is the microphone of the
/// keyboard. The typed text is never cleared by an error. A task the model
/// could fill in whole is saved at once; anything else opens the card.
class Composer extends StatefulWidget {
  const Composer({super.key, required this.api, this.now, this.onOpenModels, this.onSaved, this.onAdvice, this.inline = false});

  final LifeApi api;
  final DateTime Function()? now;

  /// Called after an event is written or a task is saved, to refresh the week.
  final VoidCallback? onSaved;

  /// Called when the phrase asks for an advice ("дай совет") instead of
  /// describing an event or a task.
  final VoidCallback? onAdvice;

  /// Opens the model settings; offered when the models are the problem.
  final VoidCallback? onOpenModels;

  /// A line among the cards of a wide window instead of a sheet at the bottom.
  final bool inline;

  @override
  State<Composer> createState() => _ComposerState();
}

class _ComposerState extends State<Composer> {
  final _text = TextEditingController();
  final _focus = FocusNode();
  final _keys = IdempotencyKeys();
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
      final draft = CardDraft.fromParsed(card);
      // «Отнести контейнеры завтра»: nothing to ask, the task goes in as it is.
      // Events still go through the card: a calendar write needs the user's OK.
      final String? done;
      if (!draft.isEvent && draft.isComplete && draft.problems(wallNow(widget.now?.call())).isEmpty) {
        final task = draft.toTask();
        await widget.api.saveTask(task, key: _keys.forPayload(jsonEncode(task.toJson())));
        if (!mounted || ticket != _ticket) return;
        setState(() => _busy = false);
        done = 'Задача «${task.title.trim()}»: ${dayTitle(task.deadline).toLowerCase()}, ${durationText(task.durationMin)}';
      } else {
        setState(() => _busy = false);
        done = await Navigator.of(context).push<String>(
          MaterialPageRoute(builder: (_) => CardScreen(api: widget.api, card: card, now: widget.now)),
        );
      }
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

  /// On a computer Enter sends and Shift + Enter starts a new line.
  KeyEventResult _onKey(FocusNode node, KeyEvent e) {
    final enter = e.logicalKey == LogicalKeyboardKey.enter || e.logicalKey == LogicalKeyboardKey.numpadEnter;
    if (!isDesktop || !enter || e is KeyUpEvent || HardwareKeyboard.instance.isShiftPressed) return KeyEventResult.ignored;
    if (_text.value.composing.isValid) return KeyEventResult.ignored; // the input method is still composing
    _send();
    return KeyEventResult.handled;
  }

  void _cancel() {
    _ticket++;
    setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final error = _error;
    final pal = Palette.of(context);
    final inline = widget.inline;
    return Container(
      decoration: inline
          ? null
          : BoxDecoration(
              color: pal.surface,
              borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
              border: Border(top: BorderSide(color: pal.line)),
              boxShadow: const [
                BoxShadow(
                  color: Color(0x140A1E1E),
                  blurRadius: 24,
                  offset: Offset(0, -8),
                ),
              ],
            ),
      child: Padding(
        padding: inline ? EdgeInsets.zero : const EdgeInsets.fromLTRB(16, 14, 16, 12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (error != null)
              _ErrorLine(error: error, onModels: widget.onOpenModels),
            if (_busy)
              Container(
                margin: const EdgeInsets.only(bottom: 10),
                padding: const EdgeInsets.fromLTRB(16, 14, 8, 6),
                decoration: BoxDecoration(
                  color: pal.container,
                  borderRadius: BorderRadius.circular(20),
                ),
                child: Row(
                  children: [
                    SizedBox(
                      width: 28,
                      height: 28,
                      child: CircularProgressIndicator(
                        strokeWidth: 3,
                        color: pal.primary,
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Text.rich(
                        TextSpan(
                          children: [
                            const TextSpan(
                              text: 'Разбираю фразу…',
                              style: TextStyle(fontWeight: FontWeight.w700),
                            ),
                            const TextSpan(text: ' это может занять до минуты'),
                          ],
                        ),
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: pal.onContainer,
                        ),
                      ),
                    ),
                    TextButton(onPressed: _cancel, child: const Text('Отмена')),
                  ],
                ),
              ),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: Focus(
                    canRequestFocus: false,
                    skipTraversal: true,
                    onKeyEvent: _onKey,
                    child: TextField(
                      controller: _text,
                      focusNode: _focus,
                      readOnly: _busy,
                      minLines: 1,
                      maxLines: 4,
                      keyboardType: TextInputType.multiline,
                      textInputAction: TextInputAction.send,
                      textCapitalization: TextCapitalization.sentences,
                      inputFormatters: [
                        LengthLimitingTextInputFormatter(maxPhraseLength),
                      ],
                      onSubmitted: (_) => _send(),
                      style: theme.textTheme.bodyLarge,
                      decoration: InputDecoration(
                        hintText: 'КВН в пятницу с 18 до 20',
                        prefixIcon: Icon(Icons.mic_none, color: pal.text3),
                        border: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(28),
                          borderSide: BorderSide.none,
                        ),
                        filled: inline ? true : null,
                        fillColor: inline ? pal.surface : null,
                        enabledBorder: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(28),
                          borderSide: inline ? BorderSide(color: pal.line, width: 2) : BorderSide.none,
                        ),
                        focusedBorder: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(28),
                          borderSide: BorderSide(color: pal.primary, width: 2),
                        ),
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 16,
                          vertical: 16,
                        ),
                        suffixIcon: _text.text.isEmpty || _busy
                            ? null
                            : IconButton(
                                tooltip: 'Очистить',
                                icon: const Icon(Icons.close, size: 20),
                                onPressed: () {
                                  _text.clear();
                                  setState(() => _error = null);
                                },
                              ),
                      ),
                    ),
                  ),
                ),
                const SizedBox(width: 10),
                IconButton.filled(
                  tooltip: 'Разобрать',
                  iconSize: 28,
                  style: IconButton.styleFrom(
                    minimumSize: const Size(56, 56),
                    backgroundColor: pal.primary,
                    foregroundColor: pal.onPrimary,
                    disabledBackgroundColor: pal.surface2,
                    disabledForegroundColor: pal.text3,
                  ),
                  icon: const Icon(Icons.arrow_upward),
                  onPressed: _canSend ? _send : null,
                ),
              ],
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(4, 8, 4, 0),
              child: Text(
                isDesktop ? 'Enter — отправить, Shift + Enter — новая строка' : 'Событие или задача с дедлайном. Можно надиктовать.',
                style: theme.textTheme.bodySmall,
              ),
            ),
          ],
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
      margin: const EdgeInsets.only(bottom: 10),
      color: theme.colorScheme.errorContainer,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
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
