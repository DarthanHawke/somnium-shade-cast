// Package config загружает настройки запуска сервера.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config содержит настройки запуска сервера.
type Config struct {
	Server   Server   `mapstructure:"server"`
	Database Database `mapstructure:"database"`
	Storage  Storage  `mapstructure:"storage"`
	Scratch  Scratch  `mapstructure:"scratch"`
	FfProber FfProber `mapstructure:"ffprober"`
	Security Security `mapstructure:"security"`
	Logs     Logs     `mapstructure:"Logs"`
}

// Server конфиг для HTTP сервера.
type Server struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

// Database конфиг для подключения к бд.
type Database struct {
	Path string `mapstructure:"path"`
}

// Storage конфиг c директорией для медиафайлов.
type Storage struct {
	Path string `mapstructure:"path"`
}

// Scratch задаёт максимальный размер одного файла при загрузке в память.
type Scratch struct {
	Size int64 `mapstructure:"size"`
}

// FfProber задаёт путь к исполняемому файлу ffprobe.
// Если Path пуст, ffprobe ищется в PATH.
type FfProber struct {
	Path string `mapstructure:"path"`
}

// Security конфиг для mTLS.
type Security struct {
	// ToDo Security
	CertDir string `mapstructure:"cert_dir"`
}

// Logs конфиг для хранения логов.
type Logs struct {
	LogFile    string `mapstructure:"log_file"`
	MaxSize    int    `mapstructure:"log_max_size"`
	MaxBackups int    `mapstructure:"log_max_backups"`
	MaxAge     int    `mapstructure:"log_max_age"`
	AddSource  bool   `mapstructure:"log_source"`
}

// Load загружает настройки сервера.
// Переменные окружения имеют приоритет над файлом и значениями по умолчанию.
// Если configPath пуст, файл конфигурации не читается.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Настройки по умолчанию
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("database.path", "./data/repository/ssc.db")
	v.SetDefault("storage.path", "./data/audio")
	v.SetDefault("scratch.size", 512*1024*1024)
	v.SetDefault("ffprober.path", "")
	v.SetDefault("security.cert_dir", "./data/certs")
	v.SetDefault("Logs.log_file", "")
	v.SetDefault("Logs.log_max_size", "10")
	v.SetDefault("Logs.log_max_backups", "3")
	v.SetDefault("Logs.log_max_age", "7")
	v.SetDefault("Logs.log_source", "true")

	// Переменные окружения (SSC_SERVER_PORT и т.д.)
	v.SetEnvPrefix("SSC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Читаем конфиг
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}
