import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'ssc_api.dart';

/// Расширения, которые принимают выбор файлов и сканирование папок.
const audioExtensions = [
  'mp3',
  'flac',
  'm4a',
  'm4b',
  'alac',
  'aac',
  'ogg',
  'oga',
  'opus',
  'wav',
  'aif',
  'aiff',
  'aifc',
  'wma',
  'ape',
  'wv',
  'dsf',
  'dff',
  'ac3',
  'mka',
  'amr',
  'caf',
  'mp2',
  'mpc',
  'tta',
];

/// Подходит ли расширение файла для отправки на сервер.
///
/// Фактический формат и наличие аудиопотока проверяет сервер.
bool isAudioFile(XFile file) {
  final dot = file.name.lastIndexOf('.');
  return dot >= 0 &&
      audioExtensions.contains(file.name.substring(dot + 1).toLowerCase());
}

/// Аудиофайлы в папке и её подпапках в порядке путей.
///
/// [path] задаёт корень обхода. `skipped` считает файлы других типов,
/// а `unreadable` — папки, которые не удалось перечислить.
Future<({List<XFile> files, int skipped, int unreadable})> scanAudioFolder(
  String path,
) async {
  final files = <XFile>[];
  var skipped = 0;
  var unreadable = 0;
  final directories = [Directory(path)];
  while (directories.isNotEmpty) {
    final directory = directories.removeLast();
    try {
      // Обходим папки по одной. Ошибка чтения одной папки оставляет остальные в очереди.
      await for (final entry in directory.list(followLinks: false)) {
        if (entry is Directory) {
          directories.add(entry);
        } else if (entry is File) {
          final file = XFile(entry.path);
          if (isAudioFile(file)) {
            files.add(file);
          } else {
            skipped++;
          }
        }
      }
    } on FileSystemException {
      unreadable++;
    }
  }
  files.sort((a, b) => a.path.compareTo(b.path));
  return (files: files, skipped: skipped, unreadable: unreadable);
}

class _UploadTask {
  _UploadTask(this.file, this.key);

  final XFile file;
  // Идентификатор запроса сохраняется при любых повторах отправки этого файла.
  final String key;
  TrackUpload? accepted;
}

/// Ошибка отдельного файла, которую можно показать в истории загрузки.
class UploadFailure {
  const UploadFailure._(this._task, this.reason, {required this.canRetry});

  final _UploadTask _task;
  final String reason;

  /// Можно ли продолжить отправку или опрос уже принятого файла.
  final bool canRetry;

  String get fileName => _task.file.name;
}

/// Последовательная очередь файлов для одной открытой библиотеки.
///
/// Как только сервер принимает файл, очередь опрашивает его статус до
/// завершения обработки. При закрытии отменяет передачу текущего файла и
/// дальнейший опрос. Сервер продолжает уже принятую обработку самостоятельно.
class UploadQueue extends ChangeNotifier {
  UploadQueue(this._api);

  final SscApi _api;
  final _pending = Queue<_UploadTask>();
  final _failures = <UploadFailure>[];
  final _random = Random.secure();
  _UploadTask? _current;
  Timer? _timer;
  Completer<void>? _delay;
  Completer<void>? _requestAbort;
  bool _disposed = false;
  bool _running = false;
  int _total = 0;
  int _ready = 0;
  int _duplicates = 0;
  String? _status;

  int get total => _total;
  int get ready => _ready;
  int get duplicates => _duplicates;

  /// Файлы, у которых есть результат, включая дубликаты и ошибки.
  int get finished => _ready + _duplicates + _failures.length;
  int get failureCount => _failures.length;

  /// Снимок списка ошибок для отображения в интерфейсе.
  List<UploadFailure> get failures => List.unmodifiable(_failures);
  bool get canRetry => _failures.any((failure) => failure.canRetry);
  String? get currentFile => _current?.file.name;
  String? get status => _status;

  /// Добавляет [files] в конец очереди с учётом порядка выбора.
  void add(Iterable<XFile> files) {
    for (final file in files) {
      final key = base64UrlEncode(
        List.generate(24, (_) => _random.nextInt(256)),
      );
      _pending.add(_UploadTask(file, key));
      _total++;
    }
    _start();
  }

  /// Повторяет восстанавливаемые ошибки, сохраняя ключи и серверные статусы.
  void retryFailures() {
    _failures.removeWhere((failure) {
      if (!failure.canRetry) return false;
      _pending.add(failure._task);
      return true;
    });
    _start();
  }

  void _start() {
    if (_running) {
      notifyListeners();
    } else if (_pending.isNotEmpty) {
      unawaited(_run());
    }
  }

  Future<void> _run() async {
    _running = true;
    while (!_disposed && _pending.isNotEmpty) {
      final task = _pending.removeFirst();
      _current = task;
      try {
        final upload = await _process(task);
        if (_disposed || upload == null) break;
        switch (upload.status) {
          case UploadStatus.ready:
            _ready++;
          case UploadStatus.failed when upload.errorCode == 'duplicate_audio':
            _duplicates++;
          case UploadStatus.failed:
            _failures.add(
              UploadFailure._(task, _describe(upload), canRetry: false),
            );
          case UploadStatus.queued || UploadStatus.processing:
            throw StateError('Загрузка ещё обрабатывается.');
        }
      } catch (error) {
        if (_disposed) break;
        final code = error is SscApiException ? error.statusCode : null;
        _failures.add(
          UploadFailure._(
            task,
            error.toString(),
            canRetry: code == null || code >= 500,
          ),
        );
      }
      _current = null;
      _status = null;
      notifyListeners();
    }
    _current = null;
    _running = false;
  }

  Future<TrackUpload?> _process(_UploadTask task) async {
    var transientErrors = 0;
    if (task.accepted case final upload?) _setStatus(_describe(upload));
    while (!_disposed) {
      try {
        final accepted = task.accepted;
        if (accepted == null) _setStatus('Отправляю файл…');
        final upload = accepted == null
            ? await _send(task)
            : await _api.getUpload(accepted.id);
        task.accepted = upload;
        if (_disposed || upload.finished) return upload;
        transientErrors = 0;
        _setStatus(_describe(upload));
        await _wait(const Duration(seconds: 2));
      } catch (error) {
        if (_disposed) return null;
        final response = error is SscApiException ? error : null;
        if (response?.statusCode == 503 || response?.statusCode == 429) {
          _setStatus('Сервер занят. Жду свободное место…');
          await _wait(
            Duration(seconds: max(1, response!.retryAfterSeconds ?? 5)),
          );
          continue;
        }
        final temporary =
            error is http.ClientException ||
            error is TimeoutException ||
            (response?.statusCode ?? 0) >= 500;
        if (!temporary || ++transientErrors > 2) rethrow;
        _setStatus('Повторяю запрос после временной ошибки…');
        await _wait(const Duration(seconds: 3));
      }
    }
    return null;
  }

  void _setStatus(String message) {
    if (_status == message) return;
    _status = message;
    notifyListeners();
  }

  String _describe(TrackUpload upload) => switch (upload.status) {
    UploadStatus.queued => 'Файл принят и ждёт обработки.',
    UploadStatus.processing => 'Сервер обрабатывает файл…',
    UploadStatus.ready => 'Трек добавлен в библиотеку.',
    UploadStatus.failed => switch (upload.errorCode) {
      'target_unavailable' => 'Трек для загрузки больше недоступен.',
      'unsupported_audio' => 'Сервер не смог прочитать аудиофайл.',
      _ => 'Сервер не смог обработать файл.',
    },
  };

  Future<TrackUpload> _send(_UploadTask task) async {
    final abort = Completer<void>();
    _requestAbort = abort;
    try {
      return await _api.upload(task.file, task.key, abortTrigger: abort.future);
    } finally {
      if (!abort.isCompleted) abort.complete();
      _requestAbort = null;
    }
  }

  Future<void> _wait(Duration duration) async {
    if (_disposed) return;
    final delay = Completer<void>();
    _delay = delay;
    _timer = Timer(duration, delay.complete);
    await delay.future;
    _timer = null;
    _delay = null;
  }

  @override
  void dispose() {
    _disposed = true;
    _requestAbort?.complete();
    _timer?.cancel();
    if (_delay case final delay? when !delay.isCompleted) delay.complete();
    super.dispose();
  }
}
