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

	container, err := container.NewContainer(config.DBPath, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("unable create container")
		return
	}

	control := controller.NewController(container, log.With().Str("logger", "controller").Logger())

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
	grpcServer.GracefulStop()
	<-control.Shutdown()
	log.Info().Msg("exited gracefully")
}

func setupLogger() {
	log.Logger = log.Logger.
		Level(zerolog.InfoLevel).
		With().
		Caller().
		Logger()

	level := strings.ToLower(os.Getenv("LEVEL"))
	if level == "" {
		return
	}

	log.Logger = log.Logger.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: zerolog.TimeFieldFormat,
	})

	switch level {
	case "trace":
		log.Logger = log.Level(zerolog.TraceLevel)
	case "debug":
		log.Logger = log.Level(zerolog.DebugLevel)
	case "info":
		log.Logger = log.Level(zerolog.InfoLevel)
	case "warn":
		log.Logger = log.Level(zerolog.WarnLevel)
	case "error":
		log.Logger = log.Level(zerolog.ErrorLevel)
	case "fatal":
		log.Logger = log.Level(zerolog.FatalLevel)
	case "panic":
		log.Logger = log.Level(zerolog.PanicLevel)
	case "nolevel":
		log.Logger = log.Level(zerolog.NoLevel)
	case "disabled":
		log.Logger = log.Level(zerolog.Disabled)
	}
}
