package main

import (
	"context"

	"github.com/9d4/wadoh/pb"
	"google.golang.org/protobuf/proto"
)

type controllerServiceServer struct {
	pb.UnimplementedControllerServiceServer
	controller *Controller
}

func (c *controllerServiceServer) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	status, err := c.controller.Status(req.Jid)
	if err != nil {
		return nil, err
	}

	return &pb.StatusResponse{Status: status}, nil
}

func (c *controllerServiceServer) RegisterDevice(req *pb.RegisterDeviceRequest, stream pb.ControllerService_RegisterDeviceServer) error {
	qr, pairCode, jid := make(chan string), make(chan string), make(chan string)

	done, err := c.controller.RegisterNewDevice(stream.Context(), req, qr, pairCode, jid)
	if err != nil {
		return err
	}

	go func() {
		select {
		case j := <-jid:
			stream.Send(&pb.RegisterDeviceResponse{
				LoggedIn: proto.Bool(true),
				Jid:      &j,
			})
			close(qr)
			close(pairCode)

		case <-stream.Context().Done():
			close(qr)
			close(pairCode)

		case <-done:
			close(qr)
			close(pairCode)
		}
	}()

	go func() {
		for code := range pairCode {
			stream.Send(&pb.RegisterDeviceResponse{
				PairCode: proto.String(code),
			})
		}
	}()

	for qr := range qr {
		stream.Send(&pb.RegisterDeviceResponse{
			Qr: proto.String(qr),
		})
	}

	return nil
}
