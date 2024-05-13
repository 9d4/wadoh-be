package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/9d4/wadoh/pb"
)

var konf = koanf.New(".")

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: zerolog.TimeFieldFormat,
	}).Level(zerolog.InfoLevel)

	loadConfig()

	if konf.Bool("development") {
		log.Logger = log.Level(zerolog.TraceLevel)
		log.Debug().Msg("development mode")
	}
}

func loadConfig() {
	konf.Load(structs.Provider(defaultDBConfig, "koanf"), nil)
	konf.Set("grpc_port", 50051)

	envCbFn := func(s string) string {
		return strings.ReplaceAll(strings.ToLower(s), "__", ".")
	}

	err := konf.Load(file.Provider(".env"), dotenv.ParserEnv("", "__", envCbFn))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Err(err).Send()
	}

	// Environment values will take precedence over .env files
	err = konf.Load(env.Provider("", "__", envCbFn), nil)
	if err != nil {
		log.Err(err).Send()
	}
}

func main() {
	var dbConfig *DBConfig
	if err := konf.Unmarshal("", &dbConfig); err != nil {
		log.Fatal().Err(err).Send()
	}

	container, err := NewContainer(dbConfig, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("unable create container")
	}

	controller := NewController(container, log.With().Str("logger", "controller").Logger())

	go controller.loop()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", konf.Int("grpc_port")))
	if err != nil {
		panic(err)
	}

	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)

	controllerServer := &controllerServiceServer{controller: controller}
	pb.RegisterControllerServiceServer(grpcServer, controllerServer)

	log.Info().Str("addr", lis.Addr().String()).Msg("gRPC Listening")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal().Err(err).Send()
	}
}
