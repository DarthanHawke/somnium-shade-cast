import 'dart:async';
import 'dart:convert';

import 'package:material_ui/material_ui.dart';
import 'package:mpv_audio_kit/mpv_audio_kit.dart'
    show EndFileError, Loop, Media, Player, PlayerError, Playlist;

import 'playback_bar.dart';
import 'ssc_api.dart';
import 'upload_panel.dart';

enum _Collection { tracks, albums, artists }

enum _TrackAction { album, artist, remove }

typedef _Entry = ({_Collection collection, String id});

/// Библиотека выбранного профиля и состояние воспроизведения его сеанса.
///
/// Внутренние страницы альбома и исполнителя используют один плеер, поэтому
/// переход назад из этих страниц сохраняет очередь воспроизведения.
class LibraryScreen extends StatefulWidget {
  const LibraryScreen({
    super.key,
    required this.api,
    required this.displayName,
    required this.initialTracks,
  });

  final SscApi api;
  final String displayName;
  final List<Track> initialTracks;

  @override
  State<LibraryScreen> createState() => _LibraryScreenState();
}

class _LibraryScreenState extends State<LibraryScreen> {
  final _player = Player();
  final _searchField = TextEditingController();
  final _pages = <_Entry>[];
  late final StreamSubscription<Playlist> _playlistChanges;
  late final StreamSubscription<PlayerError> _playbackErrors;

  late List<Track> _tracks;
  List<Track> _matches = [];
  String _query = '';
  // Поколения запросов защищают состояние от запоздалых HTTP-ответов.
  int _searchVersion = 0;
  int _libraryVersion = 0;
  Timer? _searchDelay;
  bool _searching = false;
  bool _refreshing = false;
  bool _removing = false;
  bool _changingPlayback = false;
  bool _shuffle = false;
  bool _playingAlbum = false;
  String? _error;
  String? _searchError;

  bool get _playerBusy => _changingPlayback || _removing;

  Track? get _playingTrack {
    final track = _player.state.playback.playlist.current?.extras?['track'];
    return track is Track ? track : null;
  }

  @override
  void initState() {
    super.initState();
    _tracks = widget.initialTracks;
    _playlistChanges = _player.stream.playlist.listen((_) {
      if (mounted) setState(() {});
    });
    _playbackErrors = _player.stream.error.listen((error) {
      if (mounted && error is EndFileError) {
        setState(() => _error = 'Не удалось воспроизвести трек.');
      }
    });
  }

  @override
  void dispose() {
    _searchDelay?.cancel();
    _searchField.dispose();
    unawaited(_playlistChanges.cancel());
    unawaited(_playbackErrors.cancel());
    unawaited(_player.dispose());
    super.dispose();
  }

  void _search(
    String text, {
    bool immediate = false,
    bool keepResults = false,
  }) {
    _searchDelay?.cancel();
    final query = text.trim();
    final version = ++_searchVersion;
    setState(() {
      _query = query;
      if (!keepResults) _matches = [];
      _searching = query.isNotEmpty && !keepResults;
      _searchError = null;
    });
    if (query.isEmpty) return;
    if (utf8.encode(query).length > 256) {
      setState(() {
        _searching = false;
        _searchError = 'Слишком длинный запрос. Сократи строку поиска.';
      });
      return;
    }
    _searchDelay = Timer(
      immediate ? Duration.zero : const Duration(milliseconds: 300),
      () async {
        try {
          final tracks = await widget.api.listTracks(query: query);
          if (mounted && version == _searchVersion) {
            setState(() => _matches = tracks);
          }
        } catch (error) {
          if (mounted && version == _searchVersion) {
            setState(() => _searchError = 'Поиск: $error');
          }
        } finally {
          if (mounted && version == _searchVersion) {
            setState(() => _searching = false);
          }
        }
      },
    );
  }

  Future<void> _refresh({bool quiet = false}) async {
    final version = ++_libraryVersion;
    if (!quiet) {
      setState(() {
        _refreshing = true;
        _error = null;
      });
    }
    try {
      final tracks = await widget.api.listTracks();
      if (!mounted || version != _libraryVersion) return;
      setState(() => _tracks = tracks);
      if (_query.isNotEmpty) {
        _search(_query, immediate: true, keepResults: quiet);
      }
    } catch (error) {
      if (mounted && version == _libraryVersion) {
        setState(() => _error = 'Библиотека: $error');
      }
    } finally {
      if (mounted && version == _libraryVersion && _refreshing) {
        setState(() => _refreshing = false);
      }
    }
  }

  Future<void> _playerCommand(Future<void> Function() action) async {
    if (_playerBusy) return;
    setState(() {
      _changingPlayback = true;
      _error = null;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) {
        setState(() => _error = 'Не удалось выполнить команду плеера.');
      }
    } finally {
      if (mounted) setState(() => _changingPlayback = false);
    }
  }

  Future<void> _play(
    List<Track> tracks,
    int index, {
    required bool fromAlbum,
  }) => _playerCommand(() async {
    final queue = tracks
        .map(
          (track) => Media(
            widget.api.streamUrl(track).toString(),
            httpHeaders: widget.api.headers,
            extras: {'track': track},
          ),
        )
        .toList();
    await _player.setLoop(fromAlbum && _shuffle ? Loop.playlist : Loop.off);
    if (!mounted) return;
    await _player.openAll(queue, index: index, play: true);
    _playingAlbum = fromAlbum;
    // Перемешиваем уже загруженную очередь, в которой выбран текущий трек.
    if (mounted && _shuffle) await _player.setShuffle(true);
  });

  Future<void> _toggleShuffle() => _playerCommand(() async {
    final shuffle = !_shuffle;
    await _player.setLoop(_playingAlbum && shuffle ? Loop.playlist : Loop.off);
    if (!mounted) return;
    await _player.setShuffle(shuffle);
    if (mounted) setState(() => _shuffle = shuffle);
  });

  bool _belongsTo(Track track, _Entry entry) => switch (entry.collection) {
    _Collection.tracks => track.id == entry.id,
    _Collection.albums => track.album?.id == entry.id,
    _Collection.artists => track.artist.id == entry.id,
  };

  void _openPage(_Entry entry) => setState(() {
    final index = _pages.indexOf(entry);
    if (index >= 0) {
      _pages.removeRange(index + 1, _pages.length);
    } else {
      _pages.add(entry);
    }
  });

  Future<void> _remove(_Entry entry, String name) async {
    final description = switch (entry.collection) {
      _Collection.tracks => 'Трек «$name» исчезнет из твоей библиотеки.',
      _Collection.albums =>
        'Все треки альбома «$name» исчезнут из твоей библиотеки.',
      _Collection.artists =>
        'Все треки исполнителя «$name» исчезнут из твоей библиотеки.',
    };
    final approved = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Убрать из моей музыки?'),
        content: Text(description),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Отмена'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Удалить из библиотеки'),
          ),
        ],
      ),
    );
    if (!mounted || approved != true || _playerBusy) return;
    setState(() {
      _removing = true;
      _error = null;
    });
    try {
      await widget.api.removeFromLibrary(entry.collection.name, entry.id);
      if (!mounted) return;
      // Отбрасываем ответы, запрошенные до удаления, для обеих выборок.
      _libraryVersion++;
      _searchVersion++;
      _searchDelay?.cancel();
      setState(() {
        _tracks = _tracks.where((track) => !_belongsTo(track, entry)).toList();
        _matches = _matches
            .where((track) => !_belongsTo(track, entry))
            .toList();
        _pages.removeWhere(
          (page) => !_tracks.any((track) => _belongsTo(track, page)),
        );
        _refreshing = false;
        _searching = false;
      });
      try {
        await _pruneQueue(entry);
      } finally {
        // Среди отброшенных запросов могло быть обновление после загрузки.
        if (mounted) unawaited(_refresh(quiet: true));
      }
    } catch (error) {
      if (mounted) setState(() => _error = 'Удаление: $error');
    } finally {
      if (mounted) setState(() => _removing = false);
    }
  }

  Future<void> _pruneQueue(_Entry entry) async {
    final current = _playingTrack;
    if (current != null && _belongsTo(current, entry)) {
      await _player.stop();
      return;
    }
    // Удаление с конца сохраняет индексы ещё не обработанных треков.
    final queue = _player.state.playback.playlist.items;
    for (var index = queue.length - 1; index >= 0 && mounted; index--) {
      final track = queue[index].extras?['track'];
      if (track is Track && _belongsTo(track, entry)) {
        await _player.remove(index);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final detail = _pages.lastOrNull;
    final current = _playingTrack;
    final tracks = _query.isEmpty ? _tracks : _matches;
    final colors = Theme.of(context).colorScheme;

    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          automaticallyImplyLeading: false,
          title: Text(widget.displayName),
          actions: [
            TextButton.icon(
              onPressed: () => Navigator.pop(context),
              icon: const Icon(Icons.logout),
              label: const Text('Logout'),
            ),
            const SizedBox(width: 12),
          ],
        ),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 1000),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  UploadPanel(
                    api: widget.api,
                    onTracksAdded: () => _refresh(quiet: true),
                    onRefresh: _refreshing || _removing ? null : _refresh,
                  ),
                  if (_error case final error?)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Text(error, style: TextStyle(color: colors.error)),
                    ),
                  if (_refreshing || _removing) const LinearProgressIndicator(),
                  const SizedBox(height: 12),
                  if (detail == null) ...[
                    TextField(
                      controller: _searchField,
                      decoration: InputDecoration(
                        hintText: 'Найти трек, альбом или исполнителя',
                        prefixIcon: const Icon(Icons.search),
                        suffixIcon: _searchField.text.isEmpty
                            ? null
                            : IconButton(
                                tooltip: 'Очистить поиск',
                                icon: const Icon(Icons.clear),
                                onPressed: () {
                                  _searchField.clear();
                                  _search('');
                                },
                              ),
                      ),
                      onChanged: _search,
                      onSubmitted: (query) => _search(query, immediate: true),
                    ),
                    const TabBar(
                      tabs: [
                        Tab(text: 'Моя музыка'),
                        Tab(text: 'Альбомы'),
                        Tab(text: 'Исполнители'),
                      ],
                    ),
                    if (_searching) const LinearProgressIndicator(),
                    Expanded(
                      child: _searchError != null
                          ? Center(
                              child: Text(
                                _searchError!,
                                style: TextStyle(color: colors.error),
                              ),
                            )
                          : TabBarView(
                              children: [
                                _trackList(tracks),
                                _albumList(tracks),
                                _artistList(tracks),
                              ],
                            ),
                    ),
                  ] else
                    Expanded(child: _buildDetail(detail)),
                  if (current != null)
                    PlaybackBar(
                      key: ValueKey(current.id),
                      player: _player,
                      track: current,
                      busy: _playerBusy,
                      shuffle: _shuffle,
                      onToggleShuffle: _toggleShuffle,
                      onCommand: _playerCommand,
                    ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildDetail(_Entry entry) {
    final tracks = _tracks.where((track) => _belongsTo(track, entry)).toList();
    final first = tracks.firstOrNull;
    final isAlbum = entry.collection == _Collection.albums;
    final title = isAlbum ? first?.album?.title : first?.artist.name;
    if (isAlbum) tracks.sort(_compareAlbumTracks);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            onPressed: () => setState(() => _pages.removeLast()),
            icon: const Icon(Icons.arrow_back),
            label: const Text('Назад'),
          ),
        ),
        if (first != null && title != null) ...[
          ListTile(
            leading: isAlbum
                ? _cover(
                    tracks.where((track) => track.hasCover).firstOrNull ??
                        first,
                  )
                : const Icon(Icons.person_outline),
            title: Text(
              title,
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            subtitle: isAlbum
                ? Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      onPressed: first.artist.isKnown
                          ? () => _openPage((
                              collection: _Collection.artists,
                              id: first.artist.id,
                            ))
                          : null,
                      child: Text(first.artist.name),
                    ),
                  )
                : null,
            trailing: _removeMenu(entry, title),
          ),
          const Divider(),
          Expanded(
            child: isAlbum
                ? _trackList(tracks, fromAlbum: true)
                : _albumList(
                    tracks,
                    emptyMessage:
                        'У исполнителя пока нет альбомов в твоей библиотеке.',
                  ),
          ),
        ] else
          const Expanded(
            child: Center(child: Text('В библиотеке больше нет этих треков.')),
          ),
      ],
    );
  }

  int _compareAlbumTracks(Track left, Track right) {
    final disc = left.discNo.compareTo(right.discNo);
    if (disc != 0) return disc;

    // Треки без номера идут после пронумерованных треков того же диска.
    final number = switch ((left.trackNo, right.trackNo)) {
      (null, null) => 0,
      (null, _) => 1,
      (_, null) => -1,
      (final int leftNo, final int rightNo) => leftNo.compareTo(rightNo),
    };
    if (number != 0) return number;
    final title = left.title.compareTo(right.title);
    return title == 0 ? left.id.compareTo(right.id) : title;
  }

  Widget _trackList(List<Track> tracks, {bool fromAlbum = false}) {
    if (tracks.isEmpty) return _empty();
    final current = _playingTrack;
    return ListView.builder(
      itemCount: tracks.length,
      itemBuilder: (context, index) {
        final track = tracks[index];
        final album = track.album;
        return ListTile(
          leading: _cover(track),
          selected: track.id == current?.id,
          title: Text(track.title),
          subtitle: Text(track.artist.name),
          onTap: _playerBusy
              ? null
              : () => _play(tracks, index, fromAlbum: fromAlbum),
          trailing: PopupMenuButton<_TrackAction>(
            tooltip: 'Действия с треком',
            enabled: !_removing,
            onSelected: (action) {
              switch (action) {
                case _TrackAction.album:
                  _openPage((collection: _Collection.albums, id: album!.id));
                case _TrackAction.artist:
                  _openPage((
                    collection: _Collection.artists,
                    id: track.artist.id,
                  ));
                case _TrackAction.remove:
                  unawaited(
                    _remove((
                      collection: _Collection.tracks,
                      id: track.id,
                    ), track.title),
                  );
              }
            },
            itemBuilder: (context) => [
              PopupMenuItem(
                value: _TrackAction.album,
                enabled: album != null,
                child: const Text('Перейти к альбому'),
              ),
              PopupMenuItem(
                value: _TrackAction.artist,
                enabled: track.artist.isKnown,
                child: const Text('Перейти к исполнителю'),
              ),
              PopupMenuItem(
                value: _TrackAction.remove,
                enabled: !_playerBusy,
                child: const Text('Удалить из библиотеки'),
              ),
            ],
          ),
        );
      },
    );
  }

  Widget _albumList(List<Track> tracks, {String? emptyMessage}) {
    final albums = <String, Track>{};
    for (final track in tracks) {
      if (track.album case final album?) {
        final previous = albums[album.id];
        if (previous == null || (!previous.hasCover && track.hasCover)) {
          albums[album.id] = track;
        }
      }
    }
    if (albums.isEmpty) return _empty(emptyMessage);
    final representatives = albums.values.toList()
      ..sort(
        (a, b) => a.album!.title.toLowerCase().compareTo(
          b.album!.title.toLowerCase(),
        ),
      );
    return ListView.builder(
      itemCount: representatives.length,
      itemBuilder: (context, index) {
        final track = representatives[index];
        final album = track.album!;
        final entry = (collection: _Collection.albums, id: album.id);
        return ListTile(
          leading: _cover(track),
          title: Text(album.title),
          subtitle: Text(track.artist.name),
          onTap: () => _openPage(entry),
          trailing: _removeMenu(entry, album.title),
        );
      },
    );
  }

  Widget _artistList(List<Track> tracks) {
    final artists = <String, Artist>{};
    for (final track in tracks) {
      if (track.artist.isKnown) artists[track.artist.id] = track.artist;
    }
    if (artists.isEmpty) return _empty();
    final items = artists.values.toList()
      ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    return ListView.builder(
      itemCount: items.length,
      itemBuilder: (context, index) {
        final artist = items[index];
        final entry = (collection: _Collection.artists, id: artist.id);
        return ListTile(
          leading: const Icon(Icons.person_outline),
          title: Text(artist.name),
          onTap: () => _openPage(entry),
          trailing: _removeMenu(entry, artist.name),
        );
      },
    );
  }

  Widget _removeMenu(_Entry entry, String title) => PopupMenuButton<bool>(
    tooltip: 'Действия',
    enabled: !_playerBusy,
    onSelected: (_) => _remove(entry, title),
    itemBuilder: (context) => const [
      PopupMenuItem(value: true, child: Text('Удалить треки из библиотеки')),
    ],
  );

  Widget _empty([String? message]) => Center(
    child: Text(
      message ??
          (_searching
              ? 'Ищу…'
              : _query.isEmpty
              ? 'Здесь пока пусто.'
              : 'Совпадений нет.'),
    ),
  );

  Widget _cover(Track track) {
    const placeholder = SizedBox(
      width: 44,
      height: 44,
      child: Icon(Icons.album_outlined),
    );
    if (!track.hasCover) return placeholder;
    return ClipRRect(
      borderRadius: BorderRadius.circular(6),
      child: Image.network(
        widget.api.coverUrl(track).toString(),
        headers: widget.api.headers,
        width: 44,
        height: 44,
        fit: BoxFit.cover,
        cacheWidth: 92,
        errorBuilder: (context, error, stackTrace) => placeholder,
      ),
    );
  }
}
