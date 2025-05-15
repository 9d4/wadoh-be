package config

import (
	"strings"

	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

var defaultDBConfig = &Config{
	Port:       50051,
	DBPath:     "store.db",
	ClientName: "Wadoh",
}

type Config struct {
	Port       uint16 `koanf:"port"`
	DBPath     string `koanf:"db_path"`
	ClientName string `koanf:"client_name"`
}

func LoadConfig() (*Config, error) {
	k := koanf.New(".")

	k.Load(structs.Provider(defaultDBConfig, "koanf"), nil)
	k.Set("grpc_port", 50051)

	k.Load(NewEnvProvider(), nil)
	c := &Config{}
	if err := k.Unmarshal("", &c); err != nil {
		return nil, err
	}
	return c, nil
}

func NewEnvProvider() koanf.Provider {
	const prefix = "WADOH_"
	envCbFn := func(s string) string {
		s = strings.TrimPrefix(s, prefix)
		s = strings.ToLower(s)
		return strings.ReplaceAll(s, "__", ".")
	}
	return env.Provider(prefix, "__", envCbFn)
}
