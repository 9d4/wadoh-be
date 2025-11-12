package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mau.fi/whatsmeow/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"

	"github.com/9d4/wadoh-be/config"
	"github.com/9d4/wadoh-be/container"
	"github.com/9d4/wadoh-be/controller"
	"github.com/9d4/wadoh-be/pb"
)

func main() {
	setupLogger()

	config, err := config.LoadConfig()
	if err != nil {
		log.Err(err).Send()
		return
	}
	store.DeviceProps.Os = proto.String(config.ClientName)

	containerLogLevel := strings.ToLower(os.Getenv("CONTAINER_LOG_LEVEL"))
	containerLogger := log.With().
		Str("logger", "container").
		Logger().
		Level(LevelToZerologLevel(containerLogLevel))
	container, err := container.NewContainer(config.DBPath, containerLogger)
	if err != nil {
		log.Fatal().Err(err).Msg("unable create container")
		return
	}

	controlLoggerLevel := strings.ToLower(os.Getenv("CONTROLLER_LOG_LEVEL"))
	controlLogger := log.With().
		Str("logger", "controller").
		Logger().
		Level(LevelToZerologLevel(controlLoggerLevel))
	control := controller.NewController(container, controlLogger)

	go control.Start()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		log.Fatal().Err(err).Msg("unabe to listen")
		return
	}

	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)

	controllerServer := controller.NewGRPCController(control)
	pb.RegisterControllerServiceServer(grpcServer, controllerServer)

	go func() {
		log.Info().Str("addr", lis.Addr().String()).Msg("gRPC Listening")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Send()
		}
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGTERM, os.Interrupt)

	<-interrupt
	log.Info().Msg("shutting down...")
	grpcServer.Stop()
	<-control.Shutdown()
	log.Info().Msg("exited gracefully")
}

func setupLogger() {
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "wadoh-be"
	}

	level := LevelToZerologLevel(strings.ToLower(os.Getenv("LEVEL")))
	if level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}
	log.Logger = log.Logger.
		Level(level).
		With().
		Str("app", appName).
		Caller().
		Logger().
		Output(os.Stdout)

	if os.Getenv("ENV") != "production" {
		log.Logger = log.Logger.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: zerolog.TimeFieldFormat,
		})
	}
}

func LevelToZerologLevel(level string) zerolog.Level {
	switch level {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	case "disabled":
		return zerolog.Disabled
	}

	return zerolog.NoLevel
}
