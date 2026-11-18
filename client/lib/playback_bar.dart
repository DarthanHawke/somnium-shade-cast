import 'package:material_ui/material_ui.dart';
import 'package:mpv_audio_kit/mpv_audio_kit.dart' show Loop, Player;

import 'ssc_api.dart';

/// Управление текущим треком и навигация в очереди принадлежащего библиотеке плеера.
class PlaybackBar extends StatefulWidget {
  const PlaybackBar({
    super.key,
    required this.player,
    required this.track,
    required this.busy,
    required this.shuffle,
    required this.onToggleShuffle,
    required this.onCommand,
  });

  final Player player;
  final Track track;
  final bool busy;
  final bool shuffle;
  final VoidCallback onToggleShuffle;

  /// Выполняет команду через общий обработчик состояния и ошибок плеера.
  final void Function(Future<void> Function()) onCommand;

  @override
  State<PlaybackBar> createState() => _PlaybackBarState();
}

class _PlaybackBarState extends State<PlaybackBar> {
  double? _dragValue;

  @override
  void didUpdateWidget(covariant PlaybackBar oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.busy) _dragValue = null;
  }

  void _seek(Duration target) {
    final player = widget.player;
    final trackId = widget.track.id;
    widget.onCommand(() async {
      // Автопереход может сменить трек, пока пользователь держит ползунок.
      final playback = player.state.playback;
      final track = playback.playlist.current?.extras?['track'];
      if (track is! Track || track.id != trackId) return;

      final duration = playback.duration;
      if (duration <= Duration.zero) return;
      final milliseconds = target.inMilliseconds.clamp(
        0,
        duration.inMilliseconds,
      );
      await player.seek(Duration(milliseconds: milliseconds.toInt()));
    });
  }

  void _skip({required bool forward}) {
    final player = widget.player;
    widget.onCommand(() async {
      final playback = player.state.playback;
      final playlist = playback.playlist;
      if (playlist.current == null || playlist.items.length < 2) return;
      if (playback.loop == Loop.playlist) {
        // Явный переход сохраняет поведение кнопок на концах очереди.
        final lastIndex = playlist.items.length - 1;
        if (forward && playlist.index == lastIndex) return player.jump(0);
        if (!forward && playlist.index == 0) return player.jump(lastIndex);
      }
      if (forward) {
        await player.next();
      } else {
        await player.previous();
      }
    });
  }

  String _time(Duration value) =>
      '${value.inMinutes}:${(value.inSeconds % 60).toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final player = widget.player;
    final playback = player.state.playback;
    final playlist = playback.playlist;
    final looping = playback.loop == Loop.playlist;
    final lastIndex = playlist.items.length - 1;
    final atFirst = playlist.index == 0;
    final atLast = playlist.index == lastIndex;
    final canSkip = !widget.busy && lastIndex > 0;
    final colors = Theme.of(context).colorScheme;
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Divider(height: 1),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 10),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Wrap(
                alignment: WrapAlignment.center,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Padding(
                    padding: const EdgeInsets.only(right: 12),
                    child: Text(
                      widget.track.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  IconButton(
                    tooltip: 'Предыдущий трек',
                    onPressed: canSkip && (looping || !atFirst)
                        ? () => _skip(forward: false)
                        : null,
                    icon: const Icon(Icons.skip_previous),
                  ),
                  IconButton(
                    tooltip: 'Назад на 10 секунд',
                    onPressed: widget.busy
                        ? null
                        : () => _seek(
                            player.state.playback.position -
                                const Duration(seconds: 10),
                          ),
                    icon: const Icon(Icons.replay_10),
                  ),
                  StreamBuilder<bool>(
                    stream: player.stream.playWhenReady,
                    initialData: playback.playWhenReady,
                    builder: (context, snapshot) {
                      final playing = snapshot.data == true;
                      return IconButton(
                        tooltip: playing ? 'Пауза' : 'Воспроизвести',
                        onPressed: widget.busy
                            ? null
                            : () => widget.onCommand(
                                () => player.state.playback.playWhenReady
                                    ? player.pause()
                                    : player.play(),
                              ),
                        icon: Icon(playing ? Icons.pause : Icons.play_arrow),
                      );
                    },
                  ),
                  IconButton(
                    tooltip: 'Вперёд на 10 секунд',
                    onPressed: widget.busy
                        ? null
                        : () => _seek(
                            player.state.playback.position +
                                const Duration(seconds: 10),
                          ),
                    icon: const Icon(Icons.forward_10),
                  ),
                  IconButton(
                    tooltip: 'Следующий трек',
                    onPressed: canSkip && (looping || !atLast)
                        ? () => _skip(forward: true)
                        : null,
                    icon: const Icon(Icons.skip_next),
                  ),
                  IconButton(
                    tooltip: widget.shuffle
                        ? 'Выключить случайный порядок'
                        : 'Включить случайный порядок',
                    isSelected: widget.shuffle,
                    color: widget.shuffle ? colors.primary : null,
                    onPressed: widget.busy ? null : widget.onToggleShuffle,
                    icon: const Icon(Icons.shuffle),
                  ),
                ],
              ),
              StreamBuilder<Duration>(
                stream: player.stream.duration,
                initialData: playback.duration,
                builder: (context, durationSnapshot) {
                  final duration = durationSnapshot.data ?? Duration.zero;
                  final maximum = duration.inMilliseconds.toDouble();
                  return StreamBuilder<Duration>(
                    stream: player.stream.position,
                    initialData: player.state.playback.position,
                    builder: (context, positionSnapshot) {
                      final position = positionSnapshot.data ?? Duration.zero;
                      final value =
                          (_dragValue ?? position.inMilliseconds.toDouble())
                              .clamp(0.0, maximum)
                              .toDouble();
                      final canSeek = maximum > 0 && !widget.busy;
                      return Row(
                        children: [
                          Text(_time(Duration(milliseconds: value.round()))),
                          Expanded(
                            child: Slider(
                              value: value,
                              max: maximum > 0 ? maximum : 1,
                              onChanged: canSeek
                                  ? (value) =>
                                        setState(() => _dragValue = value)
                                  : null,
                              onChangeEnd: canSeek
                                  ? (value) {
                                      setState(() => _dragValue = null);
                                      _seek(
                                        Duration(milliseconds: value.round()),
                                      );
                                    }
                                  : null,
                            ),
                          ),
                          Text(_time(duration)),
                        ],
                      );
                    },
                  );
                },
              ),
            ],
          ),
        ),
      ],
    );
  }
}
