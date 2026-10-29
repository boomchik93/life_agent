import 'dart:async';

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../api.dart';
import '../screen_time.dart';
import '../theme.dart';
import '../time_period.dart';
import '../timetrack.dart';
import '../usage.dart';
import '../widgets/error_view.dart';
import '../widgets/time_ui.dart';
import 'time_app.dart';

/// The apps of one category (or all of them) over a period, with the part of
/// the time that was on the screen, and the apps without a category.
class AppsScreen extends StatefulWidget {
  const AppsScreen({super.key, required this.ctx, required this.category, required this.period, this.weekShift = 0, this.categoryTotalMin});

  final TimeCtx ctx;
  final String? category; // null: all apps
  final TimePeriod period;
  final int weekShift;

  /// The minutes of the category from the server, for "из 14 ч по категории".
  final int? categoryTotalMin;

  @override
  State<AppsScreen> createState() => _AppsScreenState();
}

class _AppsScreenState extends State<AppsScreen> {
  late TimePeriod _period = widget.period;
  late Future<ScreenTime?> _future = _load();

  (DateTime, DateTime) get _range => periodRange(_period, widget.ctx.today, weekShift: widget.weekShift);

  Future<ScreenTime?> _load() async {
    final c = widget.ctx;
    if (!await c.usage.hasAccess()) return null;
    final (from, to) = _range;
    return c.screenTime(from, to);
  }

  void _reload() => setState(() => _future = _load());

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final cat = widget.category;
    final accent = cat == null ? pal.primary : timeColor(context, cat);
    return TimeScaffold(
      title: cat == null ? 'Все приложения' : '${timeCategoryNames[cat]}: приложения',
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
        children: [
          SegRow<TimePeriod>(
            value: _period,
            options: [for (final p in TimePeriod.values) (p, timePeriodNames[p]!)],
            onChanged: (p) => setState(() {
              _period = p;
              _future = _load();
            }),
          ),
          FutureBuilder<ScreenTime?>(
            future: _future,
            builder: (context, snap) {
              if (snap.connectionState != ConnectionState.done) return const LoadingList();
              if (snap.hasError) return ErrorView(error: snap.error!, onRetry: _reload);
              final st = snap.data;
              if (st == null) {
                return NoAccessView(onOpen: () => widget.ctx.usage.openAccessSettings(), onRetry: _reload);
              }
              final apps = st.inCategory(cat);
              final screen = st.minutesOf(cat);
              final unassigned = st.unassigned;
              if (apps.isEmpty && unassigned.isEmpty) {
                return Padding(
                  padding: const EdgeInsets.only(top: 48),
                  child: Column(children: [
                    Container(
                      width: 96,
                      height: 96,
                      decoration: BoxDecoration(color: pal.container, shape: BoxShape.circle),
                      child: Icon(Icons.storage_outlined, size: 48, color: pal.onContainer),
                    ),
                    const SizedBox(height: 16),
                    Text('Приложения не отслеживались', style: theme.textTheme.headlineMedium),
                    const SizedBox(height: 8),
                    Text(
                      cat == null
                          ? 'За этот период нет данных о приложениях.'
                          : 'За этот период нет данных о приложениях в категории «${timeCategoryNames[cat]}». Время из календаря и таймера при этом учтено.',
                      textAlign: TextAlign.center,
                      style: theme.textTheme.bodyLarge?.copyWith(color: pal.text2),
                    ),
                  ]),
                );
              }
              final catTotal = widget.categoryTotalMin;
              return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                if (apps.isNotEmpty)
                  TimeCard(children: [
                    Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
                      Expanded(
                        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                          Text('За экраном', style: theme.textTheme.bodySmall),
                          Text(hm(screen), style: theme.textTheme.headlineLarge?.copyWith(fontSize: 30)),
                        ]),
                      ),
                      if (catTotal != null && catTotal > 0)
                        Text('из ${hm(catTotal)}\nпо категории', textAlign: TextAlign.right, style: theme.textTheme.bodySmall),
                    ]),
                    if (catTotal != null && catTotal > 0) ...[
                      const SizedBox(height: 12),
                      ClipRRect(
                        borderRadius: BorderRadius.circular(6),
                        child: Row(children: [
                          Expanded(flex: math.max(1, math.min(screen, catTotal)), child: Container(height: 12, color: accent)),
                          Expanded(flex: math.max(1, catTotal - math.min(screen, catTotal)), child: Container(height: 12, color: pal.surface2)),
                        ]),
                      ),
                      const SizedBox(height: 6),
                      Text('Экран ${(math.min(screen, catTotal) * 100 / catTotal).round()}% · вне экрана: календарь и таймер',
                          style: theme.textTheme.bodySmall),
                    ],
                  ]),
                if (apps.isNotEmpty)
                  TimeCard(padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8), children: [
                    for (final a in apps.take(40))
                      InkWell(
                        borderRadius: BorderRadius.circular(12),
                        onTap: () async {
                          final changed = await Navigator.of(context).push<bool>(MaterialPageRoute(
                            builder: (_) => AppScreen(ctx: widget.ctx, use: a, period: _period, categoryTotalMin: screen),
                          ));
                          if (changed == true) _reload();
                        },
                        child: Padding(
                          padding: const EdgeInsets.symmetric(vertical: 10),
                          child: Row(children: [
                            AppBadge(a.pkg),
                            const SizedBox(width: 12),
                            Expanded(
                              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                                Row(children: [
                                  Expanded(child: Text(appLabel(a.pkg), maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall)),
                                  Text(hm(a.min), style: theme.textTheme.titleSmall),
                                ]),
                                const SizedBox(height: 6),
                                FillBar(fraction: screen == 0 ? 0 : a.min / screen, color: cat == null ? (a.category == null ? pal.text3 : timeColor(context, a.category!)) : accent),
                                const SizedBox(height: 4),
                                Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
                                  Text('${screen == 0 ? 0 : (a.min * 100 / screen).round()}% · телефон', style: theme.textTheme.bodySmall),
                                  DeltaText(a.delta),
                                ]),
                              ]),
                            ),
                            Icon(Icons.chevron_right, color: pal.text3),
                          ]),
                        ),
                      ),
                  ]),
                if (unassigned.isNotEmpty)
                  TimeCard(
                    borderColor: Theme.of(context).brightness == Brightness.dark ? const Color(0xFFFFCA28) : const Color(0xFFF9A825),
                    onTap: () async {
                      final changed = await Navigator.of(context).push<bool>(MaterialPageRoute(
                        builder: (_) => UnassignedScreen(ctx: widget.ctx, apps: unassigned, totalMin: st.apps.fold(0, (s, a) => s + a.min)),
                      ));
                      if (changed == true) _reload();
                    },
                    children: [
                      Row(children: [
                        const AppBadge('', warn: true),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                            Text('${unassigned.length} ${_appsWord(unassigned.length)} без категории', style: theme.textTheme.titleSmall),
                            Text('${hm(st.unassignedMinutes)} не учтены в диаграмме', style: theme.textTheme.bodySmall),
                          ]),
                        ),
                        Icon(Icons.chevron_right, color: pal.text3),
                      ]),
                    ],
                  ),
              ]);
            },
          ),
        ],
      ),
    );
  }
}

String _appsWord(int n) {
  final m10 = n % 10, m100 = n % 100;
  if (m10 == 1 && m100 != 11) return 'приложение';
  if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return 'приложения';
  return 'приложений';
}

/// Apps no category was found for: the user picks one for each and saves.
class UnassignedScreen extends StatefulWidget {
  const UnassignedScreen({super.key, required this.ctx, required this.apps, required this.totalMin});

  final TimeCtx ctx;
  final List<AppUse> apps;
  final int totalMin;

  @override
  State<UnassignedScreen> createState() => _UnassignedScreenState();
}

class _UnassignedScreenState extends State<UnassignedScreen> {
  final Map<String, String> _chosen = {};
  bool _saving = false;
  String? _error;

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      for (final e in _chosen.entries) {
        await widget.ctx.time.setAppCategory(e.key, appCategoryOf(e.value));
        widget.ctx.names[e.key] = NameCategory(category: appCategoryOf(e.value), confidence: 1, resolved: true);
      }
      unawaited(widget.ctx.keepNames());
      if (mounted) Navigator.of(context).pop(true);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final unaccounted = widget.apps.fold(0, (s, a) => s + a.min);
    final pct = widget.totalMin == 0 ? 0 : (unaccounted * 100 / widget.totalMin).round();
    return TimeScaffold(
      title: 'Без категории',
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
        children: [
          Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(color: theme.colorScheme.errorContainer.withValues(alpha: 0.0), borderRadius: BorderRadius.circular(16)),
            child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Icon(Icons.warning_amber, color: pal.text2, size: 22),
              const SizedBox(width: 10),
              Expanded(
                child: Text('Не учтено ${hm(unaccounted)}, это $pct% времени. Выбери категорию, и диаграмма станет точнее.',
                    style: theme.textTheme.bodyLarge),
              ),
            ]),
          ),
          for (final a in widget.apps)
            TimeCard(children: [
              Row(children: [
                AppBadge(a.pkg),
                const SizedBox(width: 12),
                Expanded(child: Text(appLabel(a.pkg), style: theme.textTheme.titleSmall)),
                Text(hm(a.min), style: theme.textTheme.titleSmall),
              ]),
              const SizedBox(height: 12),
              Wrap(spacing: 8, runSpacing: 8, children: [
                for (final c in appTimeCategories)
                  CatChip(
                    category: c,
                    selected: _chosen[a.pkg] == c,
                    onTap: () => setState(() => _chosen[a.pkg] = c),
                  ),
              ]),
            ]),
          if (_error != null)
            Padding(padding: const EdgeInsets.only(top: 12), child: Text(_error!, style: TextStyle(color: theme.colorScheme.error))),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _chosen.isEmpty || _saving ? null : _save,
            icon: _saving
                ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.check),
            label: const Text('Сохранить'),
          ),
        ],
      ),
    );
  }
}
