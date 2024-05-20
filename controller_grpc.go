package main

import (
	"context"

	"github.com/9d4/wadoh/pb"
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
	resc := make(chan *pb.RegisterDeviceResponse)
	done := make(chan struct{})

	err := c.controller.RegisterNewDevice(req, resc, done)
	if err != nil {
		return err
	}

	go func() {
		<-stream.Context().Done()
		select {
		case <-done:
			return
		default:
		}
		close(done)
	}()

OUTER:
	for {
		select {
		case <-done:
			close(resc)
			break OUTER
		case res := <-resc:
			if res.Jid != nil || res.LoggedIn != nil {
				stream.Send(res)
				break OUTER
			}

			stream.Send(res)
		}
	}

	return nil
}

func (c *controllerServiceServer) SendMessage(ctx context.Context, req *pb.SendMessageRequest) (*pb.Empty, error) {
	if err := c.controller.SendMessage(ctx, req); err != nil {
		return nil, err
	}

	return &pb.Empty{}, nil
}

func (c *controllerServiceServer) ReceiveMessage(req *pb.Empty, stream pb.ControllerService_ReceiveMessageServer) error {
	recv, err := c.controller.ReceiveMessage(stream.Context())
	if err != nil {
		return err
	}

	for evt := range recv {
		stream.Send(&pb.EventMessage{
			Jid:     evt.JID,
			From:    evt.From,
			Message: evt.Message,
		})
	}
	return nil
}
