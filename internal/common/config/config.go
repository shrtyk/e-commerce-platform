package config

import (
	"net"
	"net/url"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Service        Service  `env-prefix:"SERVICE_"`
	Postgres       Postgres `env-prefix:"POSTGRES_"`
	Redis          Redis
	Kafka          Kafka
	SchemaRegistry SchemaRegistry
	OTel           OTel
	LogLevel       string `env:"LOG_LEVEL" env-default:"info"`
}

type Service struct {
	Name        string `env:"NAME" env-required:"true"`
	Environment string `env:"ENV" env-default:"local"`
	HTTPAddr    string `env:"HTTP_ADDR" env-default:":8080"`
	GRPCAddr    string `env:"GRPC_ADDR" env-default:":9090"`
}

type Postgres struct {
	Host     string `env:"HOST" env-required:"true"`
	Port     string `env:"PORT" env-default:"5432"`
	Database string `env:"DB" env-required:"true"`
	User     string `env:"USER" env-required:"true"`
	Password string `env:"PASSWORD" env-required:"true"`
	SSLMode  string `env:"SSLMODE" env-default:"disable"`
}

type Redis struct {
	Addr    string `env:"REDIS_ADDR"`
	Enabled bool
}

type Kafka struct {
	Brokers string `env:"KAFKA_BROKERS" env-required:"true"`
}

type SchemaRegistry struct {
	URL string `env:"SCHEMA_REGISTRY_URL" env-required:"true"`
}

type OTel struct {
	Endpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" env-required:"true"`
	Insecure bool   `env:"OTEL_EXPORTER_OTLP_INSECURE" env-default:"false"`
}

func MustLoad() Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		panic(err)
	}

	cfg.Redis.Enabled = cfg.Redis.Addr != ""

	return cfg
}

func (p Postgres) DSN() string {
	host := p.Host
	if p.Port != "" {
		host = net.JoinHostPort(p.Host, p.Port)
	}

	url := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(p.User, p.Password),
		Host:   host,
		Path:   p.Database,
	}

	q := url.Query()
	q.Set("sslmode", p.SSLMode)
	url.RawQuery = q.Encode()
	return url.String()
}
