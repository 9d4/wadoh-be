package main

var defaultDBConfig = &DBConfig{
	DBPath: "store.db",
}

type DBConfig struct {
	DBPath string `koanf:"db_path"`
}
