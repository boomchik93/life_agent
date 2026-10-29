import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/screens/update_dialog.dart';
import 'package:life_agent/update.dart';

HttpLifeApi api(MockClient c) => HttpLifeApi(baseUrl: 'https://x.example/', token: 'secret-token', client: c);

final _apk = utf8.encode('PK not really an apk, but bytes all the same');
final _release = AppRelease(
  versionCode: 5,
  versionName: '1.0.0',
  sha256: sha256.convert(_apk).toString(),
  size: _apk.length,
);

class FakeInstaller implements Installer {
  FakeInstaller({this.installed = 4, this.allowed = true, required this.path});

  int installed;
  bool allowed;
  final String path;
  int installs = 0;
  int settingsOpened = 0;

  @override
  Future<int> installedVersion() async => installed;
  @override
  Future<String> apkPath() async => path;
  @override
  Future<bool> canInstall() async => allowed;
  @override
  Future<void> openInstallSettings() async => settingsOpened++;
  @override
  Future<void> install() async => installs++;
}

class FakeUpdateApi implements UpdateApi {
  FakeUpdateApi(this.release);

  final AppRelease? release;
  int downloads = 0;

  @override
  Future<AppRelease?> latestRelease() async => release;

  @override
  Future<void> downloadRelease(AppRelease release, String path, {void Function(int got, int total)? onProgress}) async {
    downloads++;
    await File(path).writeAsBytes(_apk);
    onProgress?.call(release.size, release.size);
  }
}

void main() {
  late Directory dir;
  setUp(() => dir = Directory.systemTemp.createTempSync('update_test'));
  tearDown(() => dir.deleteSync(recursive: true));

  test('latestRelease reads the release and treats no_release as none', () async {
    late http.Request seen;
    final a = api(MockClient((r) async {
      seen = r;
      return http.Response(
          '{"version_code":5,"version_name":"1.0.0","sha256":"${_release.sha256}","size":${_apk.length}}', 200);
    }));
    final r = (await a.latestRelease())!;
    expect(seen.url.toString(), 'https://x.example/v1/app/latest');
    expect(seen.headers['Authorization'], 'Bearer secret-token');
    expect(r.versionCode, 5);
    expect(r.label, '1.0.0 (5)');

    final none = api(MockClient((r) async => http.Response('{"error":"no release","code":"no_release"}', 404)));
    expect(await none.latestRelease(), isNull);
  });

  test('downloadRelease writes the APK and checks its hash', () async {
    final path = '${dir.path}/a.apk';
    late http.Request seen;
    final progress = <int>[];
    final a = api(MockClient((r) async {
      seen = r;
      return http.Response.bytes(_apk, 200);
    }));
    await a.downloadRelease(_release, path, onProgress: (got, _) => progress.add(got));
    expect(seen.url.path, '/v1/app/apk');
    expect(seen.headers['Authorization'], 'Bearer secret-token');
    expect(File(path).readAsBytesSync(), _apk);
    expect(progress.last, _apk.length);
  });

  test('a broken download is deleted and reported', () async {
    final path = '${dir.path}/a.apk';
    final a = api(MockClient((r) async => http.Response.bytes([..._apk.take(10), 0, 0], 200)));
    await expectLater(a.downloadRelease(_release, path),
        throwsA(isA<ApiException>().having((e) => e.message, 'message', contains('повреждён'))));
    expect(File(path).existsSync(), isFalse);

    final denied = api(MockClient((r) async => http.Response('{"error":"unauthorized"}', 401)));
    await expectLater(denied.downloadRelease(_release, path),
        throwsA(isA<ApiException>().having((e) => e.problem, 'problem', ApiProblem.unauthorized)));
    expect(File(path).existsSync(), isFalse);
  });

  test('newerRelease offers only a newer version and tidies the old APK', () async {
    final path = '${dir.path}/a.apk';
    final installer = FakeInstaller(installed: 4, path: path);
    expect(await newerRelease(FakeUpdateApi(_release), installer), same(_release));

    File(path).writeAsBytesSync(_apk);
    installer.installed = 5;
    expect(await newerRelease(FakeUpdateApi(_release), installer), isNull);
    expect(File(path).existsSync(), isFalse);
    expect(await newerRelease(FakeUpdateApi(null), installer), isNull);
  });

  Future<void> openDialog(WidgetTester tester, UpdateApi api, Installer installer) async {
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () => showUpdateDialog(context, _release, api, installer),
          child: const Text('open'),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('the dialog downloads and hands the APK to the installer', (tester) async {
    final updates = FakeUpdateApi(_release);
    final installer = FakeInstaller(path: '${dir.path}/a.apk');
    await openDialog(tester, updates, installer);
    expect(find.textContaining('1.0.0 (5)'), findsOneWidget);

    await tester.runAsync(() async {
      await tester.tap(find.text('Обновить'));
      await Future<void>.delayed(const Duration(milliseconds: 50));
    });
    await tester.pumpAndSettle();
    expect(updates.downloads, 1);
    expect(installer.installs, 1);
    expect(find.byType(UpdateDialog), findsNothing);
  });

  testWidgets('without the permission the dialog sends the user to the settings', (tester) async {
    final updates = FakeUpdateApi(_release);
    final installer = FakeInstaller(allowed: false, path: '${dir.path}/a.apk');
    await openDialog(tester, updates, installer);

    await tester.tap(find.text('Обновить'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Разреши один раз'), findsOneWidget);
    await tester.tap(find.text('Открыть настройки'));
    expect(installer.settingsOpened, 1);
    expect(updates.downloads, 0);

    installer.allowed = true;
    await tester.runAsync(() async {
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await Future<void>.delayed(const Duration(milliseconds: 50));
    });
    await tester.pumpAndSettle();
    expect(updates.downloads, 1);
    expect(installer.installs, 1);
  });
}
