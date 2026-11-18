// Package config загружает настройки запуска сервера.
package config

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/viper"
)

const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

// Config содержит настройки запуска сервера.
type Config struct {
	AdminName     string   `mapstructure:"admin_name"`
	AdminPassword string   `mapstructure:"admin_password"`
	Env           string   `mapstructure:"env"`
	Server        Server   `mapstructure:"server"`
	Database      Database `mapstructure:"database"`
	Storage       Storage  `mapstructure:"storage"`
	Scratch       Scratch  `mapstructure:"scratch"`
	FfProber      FfProber `mapstructure:"ffprober"`
	Security      Security `mapstructure:"security"`
	Logs          Logs     `mapstructure:"logs"`
}

// Server задаёт адрес HTTP-сервера и время чтения одного запроса.
type Server struct {
	Port        int           `mapstructure:"port"`
	Host        string        `mapstructure:"host"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
}

// Database задаёт файловый путь SQLite.
type Database struct {
	Path string `mapstructure:"path"`
}

// Storage задаёт каталог зашифрованных файлов.
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

// Security содержит настройки сертификатов.
type Security struct {
	CertDir string `mapstructure:"cert_dir"`
}

// Logs задаёт вывод в файл и параметры ротации.
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
	v.SetDefault("env", EnvDev)
	v.SetDefault("admin_name", "")
	v.SetDefault("admin_password", "")
	v.SetDefault("server.read_timeout", 10*time.Minute)
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("database.path", "./data/repository/ssc.db")
	v.SetDefault("storage.path", "./data/audio")
	v.SetDefault("scratch.size", 512*1024*1024)
	v.SetDefault("ffprober.path", "")
	v.SetDefault("security.cert_dir", "./data/certs")
	v.SetDefault("logs.log_file", "")
	v.SetDefault("logs.log_max_size", 10)
	v.SetDefault("logs.log_max_backups", 3)
	v.SetDefault("logs.log_max_age", 7)
	v.SetDefault("logs.log_source", true)

	// Переменные окружения (SSC_SERVER_PORT и т.д.)
	v.SetEnvPrefix("SSC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AllowEmptyEnv(true)
	v.AutomaticEnv()

	// Читаем конфиг
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if cfg.Env == "prod" {
		cfg.Env = EnvProd
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (cfg Config) validate() error {
	switch {
	case cfg.Env != EnvDev && cfg.Env != EnvProd:
		return fmt.Errorf("env must be dev, prod or prod (SSC_ENV)")
	case cfg.AdminName != strings.TrimSpace(cfg.AdminName) ||
		strings.ContainsRune(cfg.AdminName, ':') ||
		strings.IndexFunc(cfg.AdminName, unicode.IsControl) >= 0:
		return fmt.Errorf("SSC_ADMIN_NAME must not contain a colon, control characters or surrounding whitespace")
	case cfg.Server.ReadTimeout <= 0:
		return fmt.Errorf("server.read_timeout must be positive (SSC_SERVER_READ_TIMEOUT)")
	case strings.TrimSpace(cfg.Server.Host) == "":
		return fmt.Errorf("server.host is required (SSC_SERVER_HOST)")
	case cfg.Server.Port < 1 || cfg.Server.Port > 65535:
		return fmt.Errorf("server.port must be between 1 and 65535 (SSC_SERVER_PORT)")
	case strings.TrimSpace(cfg.Database.Path) == "":
		return fmt.Errorf("database.path is required (SSC_DATABASE_PATH)")
	case strings.TrimSpace(cfg.Storage.Path) == "":
		return fmt.Errorf("storage.path is required (SSC_STORAGE_PATH)")
	case cfg.Scratch.Size < 1 || cfg.Scratch.Size == math.MaxInt64:
		return fmt.Errorf("scratch.size must be positive and less than MaxInt64 (SSC_SCRATCH_SIZE)")
	case cfg.Logs.MaxSize < 0 || cfg.Logs.MaxBackups < 0 || cfg.Logs.MaxAge < 0:
		return fmt.Errorf("log rotation settings must be nonnegative")
	default:
		return nil
	}
}
