import 'dart:async';

import 'package:http/http.dart' as http;
import 'package:material_ui/material_ui.dart';

import 'library_screen.dart';
import 'ssc_api.dart';

/// Подключение к серверу и выбор пользователя для сеанса библиотеки.
///
/// Экран владеет HTTP-клиентом. Библиотека использует тот же транспорт
/// до выхода из сеанса.
class StartScreen extends StatefulWidget {
  const StartScreen({super.key});

  @override
  State<StartScreen> createState() => _StartScreenState();
}

typedef _Connection = ({SscApi api, List<SscUser>? devUsers});

class _StartScreenState extends State<StartScreen> {
  final _http = http.Client();
  final _serverUrl = TextEditingController(text: 'http://127.0.0.1:8080');
  final _login = TextEditingController();
  final _password = TextEditingController();

  // У prod-подключения нет списка тестовых профилей.
  _Connection? _connection;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _http.close();
    _serverUrl.dispose();
    _login.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } on TimeoutException {
      if (mounted) setState(() => _error = 'Сервер не ответил вовремя.');
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _connect() => _run(() async {
    _clearConnection();
    final url = Uri.tryParse(_serverUrl.text.trim());
    if (url == null ||
        (url.scheme != 'http' && url.scheme != 'https') ||
        url.host.isEmpty ||
        url.userInfo.isNotEmpty ||
        (url.path.isNotEmpty && url.path != '/') ||
        url.hasQuery ||
        url.hasFragment) {
      throw const SscApiException(
        'Укажи адрес сервера, например http://127.0.0.1:8080.',
      );
    }
    final api = SscApi(_http, url);
    final users = await api.connect();
    if (!mounted) return;
    setState(() => _connection = (api: api, devUsers: users));
  });

  void _clearConnection() {
    _password.clear();
    setState(() {
      _connection = null;
      _error = null;
    });
  }

  Future<void> _openLibrary(SscApi api, String displayName) async {
    final tracks = await api.listTracks();
    if (!mounted) return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (context) => LibraryScreen(
          api: api,
          displayName: displayName,
          initialTracks: tracks,
        ),
      ),
    );
  }

  Future<void> _signInAsAdmin(SscApi server) => _run(() async {
    final name = _login.text.trim();
    final password = _password.text;
    if (name.isEmpty || password.isEmpty) {
      throw const SscApiException('Введи имя администратора и пароль.');
    }
    final api = server.asAdmin(name, password);
    _password.clear();
    try {
      await _openLibrary(api, name);
    } on SscApiException catch (error) {
      if (error.statusCode == 401) {
        throw const SscApiException('Неверное имя администратора или пароль.');
      }
      rethrow;
    }
  });

  Future<void> _setActive(SscApi server, SscUser user) => _run(() async {
    final active = !user.active;
    await server.setDevUserActive(user, active);
    if (!mounted || _connection?.api != server) return;
    final users = _connection?.devUsers;
    if (users == null) return;
    setState(
      () => _connection = (
        api: server,
        devUsers: [
          for (final profile in users)
            profile.id == user.id
                ? SscUser(profile.id, profile.displayName, active)
                : profile,
        ],
      ),
    );
  });

  Future<void> _createUser(SscApi server) async {
    final formKey = GlobalKey<FormState>();
    var name = '';
    var displayName = '';
    final submitted = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Добавить пользователя'),
        content: SizedBox(
          width: 340,
          child: Form(
            key: formKey,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextFormField(
                  autofocus: true,
                  decoration: const InputDecoration(
                    labelText: 'Имя пользователя',
                  ),
                  validator: (value) => value == null || value.trim().isEmpty
                      ? 'Введи имя'
                      : null,
                  onSaved: (value) => name = value!.trim(),
                ),
                TextFormField(
                  decoration: const InputDecoration(
                    labelText: 'Отображаемое имя',
                  ),
                  onSaved: (value) => displayName = value!.trim(),
                ),
              ],
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Отмена'),
          ),
          FilledButton(
            onPressed: () {
              final form = formKey.currentState!;
              if (!form.validate()) return;
              form.save();
              Navigator.pop(context, true);
            },
            child: const Text('Создать'),
          ),
        ],
      ),
    );
    if (!mounted || submitted != true || _connection?.api != server) return;
    await _run(() async {
      final user = await server.createDevUser(name, displayName);
      if (!mounted || _connection?.api != server) return;
      final users = _connection?.devUsers;
      if (users != null) {
        setState(() => _connection = (api: server, devUsers: [...users, user]));
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final server = _connection?.api;
    final users = _connection?.devUsers;
    final colors = Theme.of(context).colorScheme;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Somnium Shade Cast'),
        actions: [
          IconButton(
            tooltip: 'О программе',
            onPressed: () => showAboutDialog(
              context: context,
              applicationName: 'Somnium Shade Cast',
              children: const [
                Text(
                  'Для воспроизведения используется libmpv под лицензией '
                  'LGPL 3.0 или более поздней версии.',
                ),
              ],
            ),
            icon: const Icon(Icons.info_outline),
          ),
        ],
      ),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 820),
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Card(
                elevation: 0,
                color: colors.secondaryContainer,
                child: const ListTile(
                  leading: Icon(Icons.info_outline),
                  title: Text('Dev-сборка для локального тестирования'),
                  subtitle: Text(
                    'На dev-сервере можно открывать тестовые профили без пароля.',
                  ),
                ),
              ),
              const SizedBox(height: 20),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  SizedBox(
                    width: 340,
                    child: TextField(
                      controller: _serverUrl,
                      enabled: !_busy,
                      decoration: const InputDecoration(
                        labelText: 'Адрес сервера',
                      ),
                      onChanged: (_) => _clearConnection(),
                      onSubmitted: (_) => _connect(),
                    ),
                  ),
                  FilledButton(
                    onPressed: _busy ? null : _connect,
                    child: const Text('Подключиться'),
                  ),
                ],
              ),
              if (_busy) ...[
                const SizedBox(height: 16),
                const LinearProgressIndicator(),
              ],
              if (_error case final error?)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(error, style: TextStyle(color: colors.error)),
                ),
              if (server != null) ...[
                const SizedBox(height: 24),
                if (users == null) ...[
                  Text(
                    'Вход администратора',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  TextField(
                    controller: _login,
                    enabled: !_busy,
                    decoration: const InputDecoration(
                      labelText: 'Имя администратора',
                    ),
                  ),
                  TextField(
                    controller: _password,
                    enabled: !_busy,
                    obscureText: true,
                    decoration: const InputDecoration(labelText: 'Пароль'),
                    onSubmitted: (_) => _signInAsAdmin(server),
                  ),
                  const SizedBox(height: 12),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: FilledButton(
                      onPressed: _busy ? null : () => _signInAsAdmin(server),
                      child: const Text('Войти'),
                    ),
                  ),
                ] else ...[
                  Wrap(
                    alignment: WrapAlignment.spaceBetween,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    spacing: 12,
                    children: [
                      Text(
                        'Тестовые пользователи',
                        style: Theme.of(context).textTheme.titleLarge,
                      ),
                      OutlinedButton.icon(
                        onPressed: _busy ? null : () => _createUser(server),
                        icon: const Icon(Icons.person_add_outlined),
                        label: const Text('Добавить пользователя'),
                      ),
                    ],
                  ),
                  if (users.isEmpty)
                    const ListTile(title: Text('Пользователей пока нет.')),
                  for (final user in users)
                    ListTile(
                      title: Text(user.displayName),
                      subtitle: Text(
                        user.active ? user.id : '${user.id} · отключён',
                      ),
                      onTap: _busy || !user.active
                          ? null
                          : () => _run(
                              () => _openLibrary(
                                server.asDevUser(user),
                                user.displayName,
                              ),
                            ),
                      trailing: TextButton(
                        onPressed: _busy
                            ? null
                            : () => _setActive(server, user),
                        child: Text(
                          user.active ? 'Деактивировать' : 'Активировать',
                        ),
                      ),
                    ),
                ],
              ],
            ],
          ),
        ),
      ),
    );
  }
}
