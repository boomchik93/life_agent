import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/screens/models_screen.dart';

import 'fakes.dart';

Future<void> open(WidgetTester t, FakeApi api) async {
  await t.pumpWidget(MaterialApp(home: ModelsScreen(api: api)));
  await t.pumpAndSettle();
}

void main() {
  testWidgets('shows quota and warns that parsing has no model', (t) async {
    await open(t, FakeApi());
    expect(find.text('Осталось 38 из 50 за сутки. Сброс в 05:00.'), findsOneWidget);
    expect(find.text('Не выбрана — без неё фразы не разбираются'), findsOneWidget);
    expect(find.text('Сохранить'), findsOneWidget);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Сохранить')).onPressed, isNull);
  });

  testWidgets('pick a model, effort appears only for a reasoning one, then save', (t) async {
    final api = FakeApi();
    await open(t, api);

    await t.tap(find.text('Не выбрана — без неё фразы не разбираются'));
    await t.pumpAndSettle();
    await t.tap(find.text('Fast'));
    await t.pumpAndSettle();
    expect(find.text('Эффорт (глубина рассуждений)'), findsNothing);
    expect(find.textContaining('нет режима рассуждений'), findsOneWidget);

    await t.tap(find.text('Fast'));
    await t.pumpAndSettle();
    await t.tap(find.text('Think'));
    await t.pumpAndSettle();
    expect(find.text('Эффорт (глубина рассуждений)'), findsOneWidget);
    await t.tap(find.text('Высокий'));
    await t.pump();

    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    final sent = api.saved.single['parse']!;
    expect(sent.model, 'vendor/think:free');
    expect(sent.effort, 'high');
    expect(find.textContaining('Сохранено'), findsOneWidget);
  });

  testWidgets('fallback chain: add, reorder, remove, limit of three', (t) async {
    final api = FakeApi()..llm = sampleLlm(parseModel: 'vendor/fast:free');
    await open(t, api);

    Future<void> add(String name) async {
      await t.ensureVisible(find.text('Добавить резервную').first);
      await t.tap(find.text('Добавить резервную').first);
      await t.pumpAndSettle();
      await t.tap(find.text(name));
      await t.pumpAndSettle();
    }

    await add('Think');
    await add('Other');
    expect(find.text('Резервные модели (2 из 3)'), findsOneWidget);

    await t.tap(find.byTooltip('Выше').last);
    await t.pump();
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.saved.single['parse']!.fallbacks, ['vendor/other:free', 'vendor/think:free']);

    await t.tap(find.byTooltip('Убрать').first);
    await t.pump();
    expect(find.text('Резервные модели (1 из 3)'), findsOneWidget);

    // Only three models fit in a chain; the fourth cannot be added.
    await add('Other');
    await add('Fast2');
    expect(find.text('Достигнут предел цепочки'), findsOneWidget);
  });

  testWidgets('a model id can be typed by hand', (t) async {
    final api = FakeApi();
    await open(t, api);
    await t.tap(find.text('Не выбрана — без неё фразы не разбираются'));
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), 'acme/new-model:free');
    await t.pump();
    await t.tap(find.text('Использовать «acme/new-model:free»'));
    await t.pumpAndSettle();
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.saved.single['parse']!.model, 'acme/new-model:free');
  });

  testWidgets('a save error stays visible and keeps the changes', (t) async {
    final api = FakeApi()..saveError = const ApiException(ApiProblem.server, 'parse: bad model id');
    await open(t, api);
    await t.tap(find.text('Не выбрана — без неё фразы не разбираются'));
    await t.pumpAndSettle();
    await t.tap(find.text('Fast'));
    await t.pumpAndSettle();
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(find.text('parse: bad model id'), findsOneWidget);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Сохранить')).onPressed, isNotNull);
  });

  testWidgets('load error offers a retry', (t) async {
    final api = FakeApi()..llmError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    await open(t, api);
    expect(find.text('Нет связи с сервером. Проверь интернет.'), findsOneWidget);
    api.llmError = null;
    await t.tap(find.text('Повторить'));
    await t.pumpAndSettle();
    expect(find.text('Основная модель'), findsWidgets);
  });

  testWidgets('phone width with large text: no overflow', (t) async {
    t.view.physicalSize = const Size(360, 780);
    t.view.devicePixelRatio = 1;
    t.platformDispatcher.textScaleFactorTestValue = 1.4;
    addTearDown(() {
      t.view.resetPhysicalSize();
      t.view.resetDevicePixelRatio();
      t.platformDispatcher.clearAllTestValues();
    });
    await open(t, FakeApi()..llm = sampleLlm(parseModel: 'vendor/think:free', fallbacks: ['vendor/fast:free', 'vendor/other:free']));
    expect(t.takeException(), isNull);
    await t.tap(find.text('Think'));
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
  });
}
