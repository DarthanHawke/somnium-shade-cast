import 'dart:async';

import 'package:file_selector/file_selector.dart';
import 'package:material_ui/material_ui.dart';

import 'ssc_api.dart';
import 'upload_queue.dart';

/// Выбор файлов и состояние фоновой загрузки в текущую библиотеку.
class UploadPanel extends StatefulWidget {
  const UploadPanel({
    super.key,
    required this.api,
    required this.onTracksAdded,
    required this.onRefresh,
  });

  final SscApi api;

  /// Запрашивает обновление библиотеки после обработки одного или нескольких файлов.
  final VoidCallback onTracksAdded;
  final VoidCallback? onRefresh;

  @override
  State<UploadPanel> createState() => _UploadPanelState();
}

class _UploadPanelState extends State<UploadPanel> {
  late final UploadQueue _queue;
  Timer? _refreshTimer;
  int _lastReady = 0;
  bool _picking = false;
  String? _selectionMessage;

  @override
  void initState() {
    super.initState();
    _queue = UploadQueue(widget.api)..addListener(_onQueueChanged);
  }

  void _onQueueChanged() {
    final tracksAdded = _queue.ready != _lastReady;
    _lastReady = _queue.ready;
    // Объединяем обновления во время загрузки и отправляем отложенное при завершении очереди.
    if (_queue.finished == _queue.total &&
        (tracksAdded || _refreshTimer != null)) {
      _refreshTimer?.cancel();
      _refreshTimer = null;
      widget.onTracksAdded();
    } else if (tracksAdded) {
      _refreshTimer ??= Timer(const Duration(seconds: 3), () {
        _refreshTimer = null;
        widget.onTracksAdded();
      });
    }
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    _queue.dispose();
    super.dispose();
  }

  Future<void> _pick({required bool folder}) async {
    if (_picking) return;
    setState(() => _picking = true);
    try {
      final List<XFile> files;
      var skipped = 0;
      var unreadable = 0;
      if (folder) {
        final path = await getDirectoryPath();
        if (path == null || !mounted) return;
        final scan = await scanAudioFolder(path);
        files = scan.files;
        skipped = scan.skipped;
        unreadable = scan.unreadable;
      } else {
        final selected = await openFiles(
          acceptedTypeGroups: const [
            XTypeGroup(label: 'Аудиофайлы', extensions: audioExtensions),
          ],
        );
        if (selected.isEmpty) return;
        files = selected.where(isAudioFile).toList();
        skipped = selected.length - files.length;
      }
      if (!mounted) return;

      final notes = [
        if (files.isEmpty) 'Аудиофайлы не найдены.',
        if (skipped > 0) 'Пропущено файлов других типов: $skipped.',
        if (unreadable > 0) 'Ошибок при чтении папок: $unreadable.',
      ];
      setState(
        () => _selectionMessage = notes.isEmpty ? null : notes.join(' '),
      );
      _queue.add(files);
    } catch (error) {
      if (mounted) {
        setState(() => _selectionMessage = 'Не удалось открыть файлы: $error');
      }
    } finally {
      if (mounted) setState(() => _picking = false);
    }
  }

  void _showFailures() {
    showDialog<void>(
      context: context,
      builder: (context) => ListenableBuilder(
        listenable: _queue,
        builder: (context, child) {
          final failures = _queue.failures;
          return AlertDialog(
            title: const Text('Ошибки загрузки'),
            content: SizedBox(
              width: 480,
              height: 250,
              child: ListView.builder(
                itemCount: failures.length,
                itemBuilder: (context, index) {
                  final failure = failures[index];
                  return ListTile(
                    title: Text(failure.fileName),
                    subtitle: Text(failure.reason),
                  );
                },
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('Закрыть'),
              ),
              if (_queue.canRetry)
                FilledButton(
                  onPressed: () {
                    _queue.retryFailures();
                    Navigator.pop(context);
                  },
                  child: const Text('Повторить попытки'),
                ),
            ],
          );
        },
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    mainAxisSize: MainAxisSize.min,
    children: [
      Wrap(
        spacing: 8,
        children: [
          MenuAnchor(
            builder: (context, controller, child) => FilledButton.icon(
              onPressed: _picking
                  ? null
                  : () => controller.isOpen
                        ? controller.close()
                        : controller.open(),
              icon: const Icon(Icons.upload_file),
              label: const Text('Загрузить треки'),
            ),
            menuChildren: [
              MenuItemButton(
                onPressed: () => _pick(folder: false),
                child: const Text('Выбрать файлы…'),
              ),
              MenuItemButton(
                onPressed: () => _pick(folder: true),
                child: const Text('Выбрать папку…'),
              ),
            ],
          ),
          TextButton.icon(
            onPressed: widget.onRefresh,
            icon: const Icon(Icons.refresh),
            label: const Text('Обновить библиотеку'),
          ),
        ],
      ),
      if (_picking) const Text('Выбираю файлы…'),
      if (_selectionMessage case final message?) Text(message),
      ListenableBuilder(
        listenable: _queue,
        builder: (context, child) {
          if (_queue.total == 0) return const SizedBox.shrink();
          final currentFile = _queue.currentFile;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (currentFile != null)
                Text(
                  '$currentFile · ${_queue.status}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              Wrap(
                spacing: 8,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Text(
                    'Обработано ${_queue.finished} из ${_queue.total} · Добавлено: ${_queue.ready} · Дубликаты: ${_queue.duplicates}',
                  ),
                  if (_queue.failureCount > 0)
                    TextButton(
                      onPressed: _showFailures,
                      child: Text('Ошибки (${_queue.failureCount})'),
                    ),
                ],
              ),
            ],
          );
        },
      ),
    ],
  );
}
