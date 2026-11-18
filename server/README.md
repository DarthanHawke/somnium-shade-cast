# Somnium Shade Cast Server

**Русский** · [English](README.en.md)

<p align="center">
  <img src="../assets/somnium-shade-cast-logo.png" alt="Somnium Shade Cast" width="600">
</p>

## Запуск сервера через Docker

Требуются Docker и Docker Compose. Команды выполняются из корня репозитория. Для новой библиотеки мастер-ключ генерируется один раз. Для восстановления нужна копия `server/.env` с исходным ключом, сохранённая отдельно от файлов библиотеки.

Генерация ключа в Windows PowerShell.

```powershell
if (-not (Test-Path -LiteralPath 'server/.env')) {
    $sscKeyBytes = [byte[]]::new(32)
    $sscRng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $sscRng.GetBytes($sscKeyBytes) } finally { $sscRng.Dispose() }

    $sscTemplate = [IO.File]::ReadAllText((Resolve-Path -LiteralPath 'server/.env.example').Path)
    $sscEnv = $sscTemplate.Replace('SSC_MASTER_KEY=', 'SSC_MASTER_KEY=' + [Convert]::ToBase64String($sscKeyBytes))
    [IO.File]::WriteAllText((Join-Path (Get-Location) 'server/.env'), $sscEnv, [Text.UTF8Encoding]::new($false))
}
```

Генерация ключа в Linux с установленным OpenSSL.

```sh
if [ ! -e server/.env ]; then
  (
    umask 077
    ssc_key=$(openssl rand -base64 32) || exit 1
    sed "s|^SSC_MASTER_KEY=$|SSC_MASTER_KEY=$ssc_key|" server/.env.example > server/.env
  )
fi
```

Оба варианта создают `server/.env` из [шаблона](.env.example), если файла ещё нет. При ручной настройке `SSC_MASTER_KEY` задаётся как 32 байта в стандартном Base64. Для существующей библиотеки нужен исходный ключ, которым были зашифрованы данные. Ранние сборки разработки использовали встроенный ключ даже при заданной переменной окружения.

Запуск и проверка сервера.

```sh
docker compose --env-file server/.env up --build --detach --wait
curl http://127.0.0.1:8080/health
```

В Windows PowerShell для проверки используется `curl.exe`. По умолчанию сервер доступен только на `127.0.0.1:8080`. Для смены порта на хосте задаётся `SSC_PUBLISH_PORT` в `server/.env`, затем этот порт указывается в клиенте. При запущенном сервере доступна документация API по адресу `http://127.0.0.1:8080/swagger/index.html`.

SQLite и зашифрованное аудио хранятся в томе `server-data`, который сохраняется при пересоздании контейнера. Команды для просмотра логов и остановки сервера.

```sh
docker compose --env-file server/.env logs -f server
docker compose --env-file server/.env down
```

## Запуск сервера без Docker

Требуются версия Go из [go.mod](go.mod), `ffprobe` из FFmpeg и `server/.env` с ключом по инструкции выше. `ffprobe` должен быть доступен через `PATH` или по пути в `SSC_FFPROBER_PATH`. Сервер запускается из каталога `server`.

```sh
cd server
go run ./cmd/somnium-shade-cast
```

## Режимы доступа

- `SSC_ENV=dev` задан в шаблоне и предназначен для локального тестирования. Открытые маршруты позволяют создавать и выбирать тестовые профили. Сервер в этом режиме должен оставаться на loopback-интерфейсе.
- `SSC_ENV=prod` включает HTTP Basic Auth для учётной записи администратора. В `server/.env` задаются `SSC_ADMIN_NAME` и `SSC_ADMIN_PASSWORD`. Учётная запись создаётся командой `init-admin` при остановленном сервере.

В Docker команда запускается из корня репозитория.

```sh
docker compose --env-file server/.env run --rm --build server init-admin
```

При локальном запуске команда выполняется из каталога `server`.

```sh
go run ./cmd/somnium-shade-cast init-admin
```

После инициализации сервер запускается обычным способом. Для подключения клиента с другой машины в режиме `prod` требуется HTTPS. Локальное HTTP-подключение также поддерживается.

## Конфигурация

Переменные процесса имеют наивысший приоритет. Затем идут необязательный `.env` в рабочем каталоге, явно выбранный файл конфигурации и встроенные значения. Путь к YAML задаётся через `--config ./config.yaml` или `SSC_CONFIG`. Относительные пути отсчитываются от рабочего каталога процесса. В Docker пути к постоянным данным должны находиться внутри `/data`.

Мастер-ключ передаётся через `SSC_MASTER_KEY`. Настройки сервера можно переопределить переменными с префиксом `SSC_`. Например, `SSC_SERVER_PORT` соответствует `server.port`, а `SSC_LOGS_LOG_FILE` задаёт путь для записи логов.

Пример необязательной конфигурации в YAML.

```yaml
server:
  port: 8090
logs:
  log_file: ./data/logs/server.log
```
