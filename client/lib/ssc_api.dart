import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:file_selector/file_selector.dart';
import 'package:http/http.dart' as http;

/// Профиль, доступный на стартовом экране в режиме разработки.
class SscUser {
  const SscUser(this.id, this.displayName, this.active);

  factory SscUser.fromJson(Map<String, dynamic> json) => switch (json) {
    {
      'public_id': String id,
      'display_name': String name,
      'is_active': bool active,
    } =>
      SscUser(id, name, active),
    _ => throw const FormatException('Некорректный профиль пользователя.'),
  };

  final String id;
  final String displayName;
  final bool active;
}

/// Исполнитель из метаданных трека.
class Artist {
  const Artist(this.id, this.name);

  final String id;
  final String name;

  /// Сервер присваивает имя `Unknown Artist`, если тег исполнителя отсутствует.
  bool get isKnown => name.isNotEmpty && name != 'Unknown Artist';
}

/// Альбом, к которому сервер привязал загруженный трек.
class Album {
  const Album(this.id, this.title);

  final String id;
  final String title;
}

/// Трек и метаданные, необходимые для библиотеки и воспроизведения.
class Track {
  const Track({
    required this.id,
    required this.title,
    required this.artist,
    required this.album,
    required this.discNo,
    required this.trackNo,
    required this.hasCover,
  });

  factory Track.fromJson(Map<String, dynamic> json) {
    if (json case {
      'public_id': String id,
      'title': String title,
      'artist': {'public_id': String artistId, 'name': String artistName},
      'disc_no': int discNo,
    }) {
      final album = switch (json['album']) {
        null => null,
        {'public_id': String id, 'title': String title} => Album(id, title),
        _ => throw const FormatException('Некорректные метаданные альбома.'),
      };
      final trackNo = json['track_no'];
      if (trackNo == null || trackNo is int) {
        return Track(
          id: id,
          title: title,
          artist: Artist(artistId, artistName),
          album: album,
          discNo: discNo,
          trackNo: trackNo,
          hasCover: json['cover_url'] is String,
        );
      }
    }
    throw const FormatException('Некорректные метаданные трека.');
  }

  final String id;
  final String title;
  final Artist artist;

  /// Альбом из ответа сервера, если трек связан с альбомом.
  final Album? album;
  final int discNo;

  /// Позиция трека на диске. Может отсутствовать в метаданных файла.
  final int? trackNo;

  /// Сервер сообщил о наличии обложки по защищённому маршруту трека.
  final bool hasCover;
}

/// Состояние фоновой обработки принятого сервером файла.
enum UploadStatus { queued, processing, ready, failed }

/// Снимок состояния загрузки, идентификатор которой выдаёт сервер.
class TrackUpload {
  const TrackUpload(this.id, this.status, this.errorCode);

  factory TrackUpload.fromJson(Map<String, dynamic> json) {
    final status = switch (json['status']) {
      'queued' => UploadStatus.queued,
      'processing' => UploadStatus.processing,
      'ready' => UploadStatus.ready,
      'failed' => UploadStatus.failed,
      _ => throw const FormatException('Неизвестный статус загрузки.'),
    };
    final errorCode = json['error_code'];
    if (json['public_id'] case final String id
        when errorCode == null || errorCode is String) {
      return TrackUpload(id, status, errorCode as String?);
    }
    throw const FormatException('Некорректные данные загрузки.');
  }

  final String id;
  final UploadStatus status;

  /// Машиночитаемая причина отказа, если сервер завершил обработку с ошибкой.
  final String? errorCode;

  /// Завершена ли обработка файла на сервере.
  bool get finished =>
      status == UploadStatus.ready || status == UploadStatus.failed;
}

/// HTTP-доступ к одному серверу SSC от имени выбранного пользователя.
///
/// Общий [http.Client] принадлежит вызывающему коду. Варианты авторизации
/// создают новые экземпляры с тем же транспортом и отдельными заголовками.
class SscApi {
  SscApi(this._client, this._baseUrl, {Map<String, String> headers = const {}})
    : headers = Map.unmodifiable(headers);

  final http.Client _client;
  final Uri _baseUrl;

  /// Заголовки авторизации для API-запросов и нативного плеера.
  final Map<String, String> headers;

  /// Доступ к защищённым маршрутам через dev-заголовок `X-User`.
  SscApi asDevUser(SscUser user) =>
      SscApi(_client, _baseUrl, headers: {'X-User': user.id});

  /// Доступ через HTTP Basic. По HTTP разрешены только адреса loopback.
  SscApi asAdmin(String name, String password) {
    final host = _baseUrl.host;
    if (_baseUrl.scheme != 'https' &&
        !{'localhost', '127.0.0.1', '::1'}.contains(host)) {
      throw const SscApiException(
        'Для входа по HTTP укажи локальный адрес. Для удалённого сервера нужен HTTPS.',
      );
    }
    final credentials = base64Encode(utf8.encode('$name:$password'));
    return SscApi(
      _client,
      _baseUrl,
      headers: {'Authorization': 'Basic $credentials'},
    );
  }

  /// Список dev-профилей или `null`, если сервер запросил HTTP Basic.
  Future<List<SscUser>?> connect() async {
    try {
      return await listDevUsers();
    } on SscApiException catch (error) {
      if (error.statusCode != 404) rethrow;
    }

    // Сам по себе 404 также возможен у неправильного адреса или старого API.
    final probe = await _request('GET', '/lib/tracks', query: {'limit': '1'});
    final challenge = probe.headers['www-authenticate']?.toLowerCase();
    if (probe.statusCode == 401 && challenge?.startsWith('basic ') == true) {
      return null;
    }
    throw const SscApiException(
      'Сервер не предоставил список dev-профилей или вход администратора. Проверь адрес и версию сервера.',
    );
  }

  /// Актуальные тестовые профили, включая отключённые.
  Future<List<SscUser>> listDevUsers() async {
    final response = await _request('GET', '/dev/users');
    return _decode(
      response,
      200,
      (json) => [
        for (final item in _jsonList(json)) SscUser.fromJson(_jsonObject(item)),
      ],
    );
  }

  /// Создаёт тестовый профиль. Пустое [displayName] заменяет сервер.
  Future<SscUser> createDevUser(String name, String displayName) async {
    final response = await _request(
      'POST',
      '/dev/users',
      body: {
        'name': name,
        if (displayName.isNotEmpty) 'display_name': displayName,
      },
    );
    return _decode(
      response,
      201,
      (json) => SscUser.fromJson(_jsonObject(json)),
    );
  }

  /// Разрешает или запрещает использование [user] в режиме разработки.
  Future<void> setDevUserActive(SscUser user, bool active) async {
    final action = active ? 'activate' : 'deactivate';
    final response = await _request('POST', '/dev/users/${user.id}/$action');
    _checkStatus(response, 200);
  }

  /// Адрес аудиопотока. Плеер передаёт [headers] и при запросах диапазонов.
  Uri streamUrl(Track track) => _baseUrl.resolve('/stream/${track.id}');

  /// Адрес защищённого ресурса обложки на том же сервере.
  Uri coverUrl(Track track) =>
      _baseUrl.resolve('/lib/tracks/${track.id}/cover');

  /// Все доступные для воспроизведения треки или результаты поиска [query].
  Future<List<Track>> listTracks({String query = ''}) async {
    const limit = 200;
    final tracks = <Track>[];
    final seenIds = <String>{};
    for (var offset = 0; ; offset += limit) {
      final response = await _request(
        'GET',
        '/lib/tracks',
        query: {
          'limit': '$limit',
          'offset': '$offset',
          if (query.isNotEmpty) 'q': query,
        },
      );
      final page = _decode(response, 200, (json) {
        final items = _jsonList(json);
        final uploaded = <Track>[];
        for (final item in items.map(_jsonObject)) {
          final status = item['status'];
          if (status is! String) {
            throw const FormatException('Отсутствует статус трека.');
          }
          if (status == 'uploaded') uploaded.add(Track.fromJson(item));
        }
        return (length: items.length, tracks: uploaded);
      });
      // При параллельных загрузках границы страниц по offset могут сместиться.
      for (final track in page.tracks) {
        if (seenIds.add(track.id)) tracks.add(track);
      }
      if (page.length < limit) return tracks;
    }
  }

  /// Убирает трек, треки альбома или треки исполнителя из библиотеки владельца.
  Future<void> removeFromLibrary(String collection, String id) async {
    final response = await _request('DELETE', '/lib/$collection/$id');
    _checkStatus(response, 204);
  }

  /// Передаёт файл потоком и возвращает состояние принятой сервером загрузки.
  ///
  /// При повторной попытке для того же файла вызывающий код сохраняет [key].
  /// [abortTrigger] прекращает передачу при закрытии клиентской очереди.
  Future<TrackUpload> upload(
    XFile file,
    String key, {
    Future<void>? abortTrigger,
  }) async {
    final request =
        http.AbortableMultipartRequest(
            'POST',
            _baseUrl.resolve('/ingest/uploads'),
            abortTrigger: abortTrigger,
          )
          ..followRedirects = false
          ..headers.addAll(headers)
          ..headers['Idempotency-Key'] = key
          ..fields['meta'] = '{}';
    // MultipartRequest пишет поля перед файлами, как ожидает сервер.
    request.files.add(
      http.MultipartFile(
        'file',
        file.openRead(),
        await file.length(),
        filename: file.name,
      ),
    );
    final response = await http.Response.fromStream(
      await _client.send(request),
    );
    return _decode(
      response,
      202,
      (json) => TrackUpload.fromJson(_jsonObject(json)),
    );
  }

  /// Состояние загрузки [id], полученного после `202 Accepted`.
  Future<TrackUpload> getUpload(String id) async {
    final response = await _request('GET', '/ingest/uploads/$id');
    return _decode(
      response,
      200,
      (json) => TrackUpload.fromJson(_jsonObject(json)),
    );
  }

  Future<http.Response> _request(
    String method,
    String path, {
    Map<String, String>? query,
    Map<String, dynamic>? body,
  }) async {
    final uri = _baseUrl.resolve(path).replace(queryParameters: query);
    const timeout = Duration(seconds: 15);
    final abort = Completer<void>();
    final request =
        http.AbortableRequest(method, uri, abortTrigger: abort.future)
          ..followRedirects = false
          ..headers.addAll(headers);
    if (body != null) {
      request.headers['Content-Type'] = 'application/json; charset=utf-8';
      request.body = jsonEncode(body);
    }
    return _client
        .send(request)
        .then(http.Response.fromStream)
        .timeout(
          timeout,
          onTimeout: () {
            abort.complete();
            throw TimeoutException('Сервер не ответил вовремя.', timeout);
          },
        );
  }

  T _decode<T>(
    http.Response response,
    int expectedStatus,
    T Function(Object?) decode,
  ) {
    _checkStatus(response, expectedStatus);
    try {
      return decode(jsonDecode(utf8.decode(response.bodyBytes)));
    } on FormatException {
      throw const SscApiException('Сервер вернул ответ в неожиданном формате.');
    }
  }

  static Map<String, dynamic> _jsonObject(Object? value) {
    if (value is Map<String, dynamic>) return value;
    throw const FormatException('Ожидался объект JSON.');
  }

  static List<Object?> _jsonList(Object? value) {
    if (value is List<Object?>) return value;
    throw const FormatException('Ожидался массив JSON.');
  }

  static int? _retryAfterSeconds(String? header) {
    if (header == null) return null;
    final seconds = int.tryParse(header);
    if (seconds != null && seconds >= 0) return seconds;
    try {
      final delay = HttpDate.parse(header).difference(DateTime.now().toUtc());
      return delay.isNegative ? 0 : (delay.inMilliseconds + 999) ~/ 1000;
    } on HttpException {
      return null;
    }
  }

  void _checkStatus(http.Response response, int expectedStatus) {
    if (response.statusCode == expectedStatus) return;
    final status = 'HTTP ${response.statusCode}';
    final retryAfter = _retryAfterSeconds(response.headers['retry-after']);
    try {
      final json = jsonDecode(utf8.decode(response.bodyBytes));
      if (json is Map && json['error'] is String) {
        throw SscApiException(
          '$status — ${json['error']}',
          response.statusCode,
          retryAfter,
        );
      }
    } on FormatException {
      // Например, прокси может вернуть HTML вместо JSON.
    }
    throw SscApiException(status, response.statusCode, retryAfter);
  }
}

/// Ошибка HTTP или несовместимый ответ сервера с сообщением для интерфейса.
class SscApiException implements Exception {
  const SscApiException(
    this.message, [
    this.statusCode,
    this.retryAfterSeconds,
  ]);

  final String message;

  /// Код ответа для HTTP-ошибок. Ошибка протокола может не иметь кода.
  final int? statusCode;

  /// Минимальная пауза перед повтором в секундах, если её задал сервер.
  final int? retryAfterSeconds;

  @override
  String toString() => message;
}
